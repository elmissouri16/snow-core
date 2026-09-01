use std::{
    env,
    fs::File,
    io::{self, Read},
    path::{Path, PathBuf},
    process::{Command, Stdio},
    sync::{
        Arc,
        mpsc::{self, TryRecvError},
    },
    thread,
    time::{Duration, Instant},
};

use base64::{Engine as _, engine::general_purpose::STANDARD as BASE64_STANDARD};
use gpui::Image;
use serde::Serialize;
use thiserror::Error;

use crate::image_safety::validate_and_cache_image;

// The child RPC frame is capped at 16 MiB. Keep aggregate raw bytes below
// 11 MiB so base64 expansion plus the JSON envelope cannot cross that bound.
pub const MAX_IMAGE_BYTES: usize = 8 << 20;
pub const MAX_TOTAL_IMAGE_BYTES: usize = 11 << 20;
pub const MAX_IMAGES: usize = 8;
pub const CLIPBOARD_TIMEOUT: Duration = Duration::from_secs(3);

const CLIPBOARD_TYPES_BYTES: usize = 64 << 10;
const COMMAND_POLL_INTERVAL: Duration = Duration::from_millis(10);

#[derive(Debug, Error)]
pub enum AttachmentError {
    #[error("image path is not a regular file: {0}")]
    NotRegularFile(PathBuf),
    #[error("image is empty")]
    Empty,
    #[error("image exceeds the {limit_mib} MiB per-image limit")]
    ImageTooLarge { limit_mib: usize },
    #[error("image is not PNG, JPEG, GIF, or WebP")]
    UnsupportedFormat,
    #[error("image cannot be decoded safely: {0}")]
    UnsafeImage(String),
    #[error("a prompt can contain at most {limit} images")]
    TooManyImages { limit: usize },
    #[error("prompt images exceed the {limit_mib} MiB aggregate limit")]
    TotalTooLarge { limit_mib: usize },
    #[error("clipboard images are unsupported on {0}")]
    UnsupportedPlatform(String),
    #[error("clipboard does not contain a supported image")]
    ClipboardEmpty,
    #[error("clipboard integration is unavailable: {0}")]
    ClipboardUnavailable(String),
    #[error("clipboard command {command} timed out")]
    CommandTimedOut { command: String },
    #[error("clipboard command {command} exceeded its {limit} byte output limit")]
    CommandOutputTooLarge { command: String, limit: usize },
    #[error("clipboard command {command} failed: {message}")]
    CommandFailed { command: String, message: String },
    #[error("could not read {path}: {source}")]
    ReadFile {
        path: PathBuf,
        #[source]
        source: io::Error,
    },
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ImageAttachment {
    label: String,
    mime_type: &'static str,
    data: Vec<u8>,
    preview: Arc<Image>,
}

impl ImageAttachment {
    pub fn from_file(path: impl AsRef<Path>) -> Result<Self, AttachmentError> {
        let path = path.as_ref();
        let file = File::open(path).map_err(|source| AttachmentError::ReadFile {
            path: path.to_path_buf(),
            source,
        })?;
        let metadata = file
            .metadata()
            .map_err(|source| AttachmentError::ReadFile {
                path: path.to_path_buf(),
                source,
            })?;
        if !metadata.is_file() {
            return Err(AttachmentError::NotRegularFile(path.to_path_buf()));
        }
        if metadata.len() > MAX_IMAGE_BYTES as u64 {
            return Err(AttachmentError::ImageTooLarge {
                limit_mib: MAX_IMAGE_BYTES >> 20,
            });
        }

        let mut data = Vec::with_capacity(metadata.len() as usize);
        file.take(MAX_IMAGE_BYTES as u64 + 1)
            .read_to_end(&mut data)
            .map_err(|source| AttachmentError::ReadFile {
                path: path.to_path_buf(),
                source,
            })?;
        let label = path
            .file_name()
            .filter(|name| !name.is_empty())
            .map(|name| name.to_string_lossy().into_owned())
            .unwrap_or_else(|| path.display().to_string());
        Self::from_bytes(label, data)
    }

    pub fn from_clipboard() -> Result<Self, AttachmentError> {
        let data = read_clipboard_image()?;
        Self::from_bytes("Clipboard image", data)
    }

    pub fn from_bytes(label: impl Into<String>, data: Vec<u8>) -> Result<Self, AttachmentError> {
        if data.is_empty() {
            return Err(AttachmentError::Empty);
        }
        if data.len() > MAX_IMAGE_BYTES {
            return Err(AttachmentError::ImageTooLarge {
                limit_mib: MAX_IMAGE_BYTES >> 20,
            });
        }
        let mime_type = image_mime_type(&data).ok_or(AttachmentError::UnsupportedFormat)?;
        let preview =
            validate_and_cache_image(mime_type, &data).map_err(AttachmentError::UnsafeImage)?;
        Ok(Self {
            label: label.into(),
            mime_type,
            data,
            preview,
        })
    }

    pub fn label(&self) -> &str {
        &self.label
    }

    pub fn mime_type(&self) -> &'static str {
        self.mime_type
    }

    pub fn len(&self) -> usize {
        self.data.len()
    }

    #[cfg(test)]
    pub fn is_empty(&self) -> bool {
        self.data.is_empty()
    }

    pub(crate) fn data(&self) -> &[u8] {
        &self.data
    }

    pub(crate) fn preview(&self) -> Arc<Image> {
        Arc::clone(&self.preview)
    }

    pub fn to_rpc_content_block(&self) -> RpcImageContentBlock {
        RpcImageContentBlock {
            kind: "image",
            mime_type: self.mime_type,
            data: BASE64_STANDARD.encode(&self.data),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct RpcImageContentBlock {
    #[serde(rename = "type")]
    pub kind: &'static str,
    pub mime_type: &'static str,
    pub data: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ImageAttachments {
    images: Vec<ImageAttachment>,
    total_bytes: usize,
}

impl ImageAttachments {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn images(&self) -> &[ImageAttachment] {
        &self.images
    }

    #[cfg(test)]
    pub fn len(&self) -> usize {
        self.images.len()
    }

    pub fn is_empty(&self) -> bool {
        self.images.is_empty()
    }

    #[cfg(test)]
    pub fn total_bytes(&self) -> usize {
        self.total_bytes
    }

    pub fn push(&mut self, image: ImageAttachment) -> Result<(), AttachmentError> {
        validate_attachment_limits(self.images.len(), self.total_bytes, image.len())?;
        self.total_bytes += image.len();
        self.images.push(image);
        Ok(())
    }

    pub fn remove(&mut self, index: usize) -> Option<ImageAttachment> {
        if index >= self.images.len() {
            return None;
        }
        let image = self.images.remove(index);
        self.total_bytes -= image.len();
        Some(image)
    }

    pub fn clear(&mut self) {
        self.images.clear();
        self.total_bytes = 0;
    }

    pub fn rpc_content_blocks(&self) -> Vec<RpcImageContentBlock> {
        self.images
            .iter()
            .map(ImageAttachment::to_rpc_content_block)
            .collect()
    }
}

pub fn validate_attachment_limits(
    existing_count: usize,
    existing_bytes: usize,
    next_bytes: usize,
) -> Result<(), AttachmentError> {
    if next_bytes > MAX_IMAGE_BYTES {
        return Err(AttachmentError::ImageTooLarge {
            limit_mib: MAX_IMAGE_BYTES >> 20,
        });
    }
    if existing_count >= MAX_IMAGES {
        return Err(AttachmentError::TooManyImages { limit: MAX_IMAGES });
    }
    if existing_bytes.saturating_add(next_bytes) > MAX_TOTAL_IMAGE_BYTES {
        return Err(AttachmentError::TotalTooLarge {
            limit_mib: MAX_TOTAL_IMAGE_BYTES >> 20,
        });
    }
    Ok(())
}

pub fn image_mime_type(data: &[u8]) -> Option<&'static str> {
    match data {
        [0x89, b'P', b'N', b'G', b'\r', b'\n', 0x1a, b'\n', ..] => Some("image/png"),
        [0xff, 0xd8, 0xff, ..] => Some("image/jpeg"),
        [b'G', b'I', b'F', b'8', b'7' | b'9', b'a', ..] => Some("image/gif"),
        [
            b'R',
            b'I',
            b'F',
            b'F',
            _,
            _,
            _,
            _,
            b'W',
            b'E',
            b'B',
            b'P',
            ..,
        ] => Some("image/webp"),
        _ => None,
    }
}

fn read_clipboard_image() -> Result<Vec<u8>, AttachmentError> {
    let deadline = Instant::now() + CLIPBOARD_TIMEOUT;
    let data = match env::consts::OS {
        "macos" => bounded_command_output(
            deadline,
            MAX_IMAGE_BYTES,
            "osascript",
            &["-l", "JavaScript", "-e", MACOS_CLIPBOARD_IMAGE_SCRIPT],
        ),
        "linux" => read_linux_clipboard_image(deadline),
        platform => return Err(AttachmentError::UnsupportedPlatform(platform.into())),
    }?;
    if data.is_empty() {
        return Err(AttachmentError::ClipboardEmpty);
    }
    Ok(data)
}

fn read_linux_clipboard_image(deadline: Instant) -> Result<Vec<u8>, AttachmentError> {
    let mut failures = Vec::new();
    let mut found_backend = false;

    if command_exists("wl-paste") {
        found_backend = true;
        match bounded_command_output(
            deadline,
            CLIPBOARD_TYPES_BYTES,
            "wl-paste",
            &["--list-types"],
        ) {
            Ok(types) => {
                if let Some(mime) = preferred_clipboard_image_type(&String::from_utf8_lossy(&types))
                {
                    match bounded_command_output(
                        deadline,
                        MAX_IMAGE_BYTES,
                        "wl-paste",
                        &["--no-newline", "--type", mime],
                    ) {
                        Ok(data) => return Ok(data),
                        Err(error) => failures.push(error.to_string()),
                    }
                } else {
                    failures.push(AttachmentError::ClipboardEmpty.to_string());
                }
            }
            Err(error) => failures.push(error.to_string()),
        }
    }

    if command_exists("xclip") {
        found_backend = true;
        match bounded_command_output(
            deadline,
            CLIPBOARD_TYPES_BYTES,
            "xclip",
            &["-selection", "clipboard", "-t", "TARGETS", "-o"],
        ) {
            Ok(types) => {
                if let Some(mime) = preferred_clipboard_image_type(&String::from_utf8_lossy(&types))
                {
                    match bounded_command_output(
                        deadline,
                        MAX_IMAGE_BYTES,
                        "xclip",
                        &["-selection", "clipboard", "-t", mime, "-o"],
                    ) {
                        Ok(data) => return Ok(data),
                        Err(error) => failures.push(error.to_string()),
                    }
                } else {
                    failures.push(AttachmentError::ClipboardEmpty.to_string());
                }
            }
            Err(error) => failures.push(error.to_string()),
        }
    }

    if !found_backend {
        return Err(AttachmentError::ClipboardUnavailable(
            "install wl-paste or xclip".into(),
        ));
    }
    if failures
        .iter()
        .all(|failure| failure == "clipboard does not contain a supported image")
    {
        return Err(AttachmentError::ClipboardEmpty);
    }
    Err(AttachmentError::ClipboardUnavailable(failures.join("; ")))
}

fn preferred_clipboard_image_type(types: &str) -> Option<&'static str> {
    let available = types.split_whitespace().collect::<Vec<_>>();
    ["image/png", "image/jpeg", "image/webp", "image/gif"]
        .into_iter()
        .find(|mime| available.contains(mime))
}

fn command_exists(name: &str) -> bool {
    let Some(path) = env::var_os("PATH") else {
        return false;
    };
    env::split_paths(&path).any(|directory| directory.join(name).is_file())
}

fn bounded_command_output(
    deadline: Instant,
    limit: usize,
    command: &str,
    args: &[&str],
) -> Result<Vec<u8>, AttachmentError> {
    if Instant::now() >= deadline {
        return Err(AttachmentError::CommandTimedOut {
            command: command.into(),
        });
    }

    let mut child = Command::new(command)
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|error| {
            AttachmentError::ClipboardUnavailable(format!("could not start {command}: {error}"))
        })?;
    let stdout = child.stdout.take().ok_or_else(|| {
        AttachmentError::ClipboardUnavailable(format!("could not capture {command} output"))
    })?;
    let (output_tx, output_rx) = mpsc::sync_channel(1);
    thread::spawn(move || {
        let _ = output_tx.send(drain_bounded(stdout, limit));
    });

    let mut status = None;
    let mut output = None;
    loop {
        if status.is_none() {
            match child.try_wait() {
                Ok(completed) => status = completed,
                Err(error) => {
                    let _ = child.kill();
                    let _ = child.wait();
                    return Err(AttachmentError::CommandFailed {
                        command: command.into(),
                        message: error.to_string(),
                    });
                }
            }
        }
        if output.is_none() {
            match output_rx.try_recv() {
                Ok(result) => output = Some(result),
                Err(TryRecvError::Empty) => {}
                Err(TryRecvError::Disconnected) => {
                    let _ = child.kill();
                    let _ = child.wait();
                    return Err(AttachmentError::CommandFailed {
                        command: command.into(),
                        message: "output reader stopped unexpectedly".into(),
                    });
                }
            }
        }
        if let Some(completed_status) = status.as_ref()
            && let Some(output) = output.take()
        {
            let (output, exceeded_limit) =
                output.map_err(|error| AttachmentError::CommandFailed {
                    command: command.into(),
                    message: error.to_string(),
                })?;
            if exceeded_limit {
                return Err(AttachmentError::CommandOutputTooLarge {
                    command: command.into(),
                    limit,
                });
            }
            if !completed_status.success() {
                return Err(AttachmentError::CommandFailed {
                    command: command.into(),
                    message: completed_status.to_string(),
                });
            }
            return Ok(output);
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            // A descendant could still own the pipe. The reader drains while retaining
            // at most limit + 1 bytes and may safely finish after this deadline error.
            return Err(AttachmentError::CommandTimedOut {
                command: command.into(),
            });
        }
        thread::sleep(
            COMMAND_POLL_INTERVAL.min(deadline.saturating_duration_since(Instant::now())),
        );
    }
}

fn drain_bounded(mut reader: impl Read, limit: usize) -> io::Result<(Vec<u8>, bool)> {
    let retained_limit = limit.saturating_add(1);
    let mut output = Vec::with_capacity(retained_limit.min(64 << 10));
    let mut buffer = [0_u8; 16 << 10];
    let mut exceeded_limit = false;

    loop {
        let read = reader.read(&mut buffer)?;
        if read == 0 {
            break;
        }
        let remaining = retained_limit.saturating_sub(output.len());
        output.extend_from_slice(&buffer[..read.min(remaining)]);
        exceeded_limit |= read > remaining || output.len() > limit;
    }
    Ok((output, exceeded_limit))
}

const MACOS_CLIPBOARD_IMAGE_SCRIPT: &str = r#"ObjC.import('AppKit'); ObjC.import('Foundation');
const p = $.NSPasteboard.generalPasteboard;
const out = $.NSFileHandle.fileHandleWithStandardOutput;
let wrote = false;
const types = ['public.png','public.jpeg','com.compuserve.gif','org.webmproject.webp'];
for (const t of types) { const d = p.dataForType(t); if (ObjC.unwrap(d) !== undefined && Number(d.length) > 0) { out.writeData(d); wrote = true; break; } }
if (!wrote) { const d = p.dataForType('public.tiff'); if (ObjC.unwrap(d) !== undefined) { const image = $.NSImage.alloc.initWithData(d); const tiff = image ? image.TIFFRepresentation : null; const r = tiff ? $.NSBitmapImageRep.imageRepWithData(tiff) : null; if (r) { const png = r.representationUsingTypeProperties(4, $({})); if (png) out.writeData(png); } } }"#;

#[cfg(test)]
mod tests {
    use std::{
        fs::{self, OpenOptions},
        io::Cursor,
        sync::atomic::{AtomicU64, Ordering},
    };

    use image::ImageFormat;

    use super::*;

    // Signature-only samples are deliberately sufficient for the isolated
    // content-sniffing test. Tests which construct attachments use a fully
    // decodable PNG generated by `valid_png`.
    const PNG_SIGNATURE: &[u8] = b"\x89PNG\r\n\x1a\ncontents";
    const JPEG_SIGNATURE: &[u8] = b"\xff\xd8\xffcontents";
    const GIF87_SIGNATURE: &[u8] = b"GIF87acontents";
    const GIF89_SIGNATURE: &[u8] = b"GIF89acontents";
    const WEBP_SIGNATURE: &[u8] = b"RIFF\x04\x00\x00\x00WEBPcontents";

    static NEXT_PATH: AtomicU64 = AtomicU64::new(1);

    struct TempPath(PathBuf);

    impl TempPath {
        fn new(extension: &str) -> Self {
            let id = NEXT_PATH.fetch_add(1, Ordering::Relaxed);
            Self(env::temp_dir().join(format!(
                "snow-desktop-attachment-{}-{id}.{extension}",
                std::process::id()
            )))
        }
    }

    impl Drop for TempPath {
        fn drop(&mut self) {
            let _ = fs::remove_file(&self.0);
        }
    }

    fn valid_png() -> Vec<u8> {
        let mut bytes = Cursor::new(Vec::new());
        image::DynamicImage::new_rgba8(1, 1)
            .write_to(&mut bytes, ImageFormat::Png)
            .unwrap();
        bytes.into_inner()
    }

    #[test]
    fn sniffs_supported_image_formats_by_content() {
        for (data, expected) in [
            (PNG_SIGNATURE, "image/png"),
            (JPEG_SIGNATURE, "image/jpeg"),
            (GIF87_SIGNATURE, "image/gif"),
            (GIF89_SIGNATURE, "image/gif"),
            (WEBP_SIGNATURE, "image/webp"),
        ] {
            assert_eq!(image_mime_type(data), Some(expected));
        }
        assert_eq!(image_mime_type(b"not an image"), None);
        assert_eq!(image_mime_type(&[]), None);
    }

    #[test]
    fn attachment_serializes_validated_bytes_and_reuses_cached_preview() {
        let png = valid_png();
        let attachment = ImageAttachment::from_bytes("shot.png", png.clone()).unwrap();
        assert_eq!(attachment.label(), "shot.png");
        assert_eq!(attachment.mime_type(), "image/png");
        assert_eq!(attachment.len(), png.len());
        assert_eq!(attachment.data(), png);
        assert!(!attachment.is_empty());
        assert_eq!(
            serde_json::to_value(attachment.to_rpc_content_block()).unwrap(),
            serde_json::json!({
                "type": "image",
                "mime_type": "image/png",
                "data": BASE64_STANDARD.encode(&png),
            })
        );

        let first = attachment.preview();
        let second = attachment.preview();
        let cloned = attachment.clone();
        assert!(Arc::ptr_eq(&first, &second));
        assert!(Arc::ptr_eq(&first, &cloned.preview()));
        assert_eq!(first.format, gpui::ImageFormat::Png);
    }

    #[test]
    fn attachment_rejects_corrupt_and_oversized_input_before_transport() {
        assert!(matches!(
            ImageAttachment::from_bytes("corrupt.png", PNG_SIGNATURE.to_vec()),
            Err(AttachmentError::UnsafeImage(_))
        ));
        assert!(matches!(
            ImageAttachment::from_bytes("too-large.png", vec![0; MAX_IMAGE_BYTES + 1]),
            Err(AttachmentError::ImageTooLarge { .. })
        ));
    }

    #[test]
    fn file_loading_uses_content_and_a_bounded_regular_file() {
        let path = TempPath::new("misleading");
        fs::write(&path.0, valid_png()).unwrap();
        let attachment = ImageAttachment::from_file(&path.0).unwrap();
        assert_eq!(attachment.mime_type(), "image/png");
        assert_eq!(
            attachment.label(),
            path.0.file_name().unwrap().to_string_lossy()
        );

        let directory_error = ImageAttachment::from_file(env::temp_dir()).unwrap_err();
        assert!(matches!(
            directory_error,
            AttachmentError::NotRegularFile(_)
        ));
    }

    #[test]
    fn file_loading_rejects_oversized_sparse_file_before_reading() {
        let path = TempPath::new("png");
        let file = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .open(&path.0)
            .unwrap();
        file.set_len(MAX_IMAGE_BYTES as u64 + 1).unwrap();
        assert!(matches!(
            ImageAttachment::from_file(&path.0),
            Err(AttachmentError::ImageTooLarge { .. })
        ));
    }

    #[test]
    fn collection_enforces_count_and_transport_budget_limits() {
        assert!(
            validate_attachment_limits(MAX_IMAGES, 0, 1)
                .is_err_and(|error| matches!(error, AttachmentError::TooManyImages { .. }))
        );
        assert!(
            validate_attachment_limits(2, MAX_TOTAL_IMAGE_BYTES - 1, 2)
                .is_err_and(|error| matches!(error, AttachmentError::TotalTooLarge { .. }))
        );
        assert!(
            validate_attachment_limits(0, 0, MAX_IMAGE_BYTES + 1)
                .is_err_and(|error| matches!(error, AttachmentError::ImageTooLarge { .. }))
        );
        assert!(validate_attachment_limits(0, 0, MAX_IMAGE_BYTES).is_ok());
        assert!(validate_attachment_limits(1, MAX_TOTAL_IMAGE_BYTES - 1, 1).is_ok());

        // Base64 expansion of the maximum aggregate raw payload leaves more
        // than one MiB for the bounded JSON-RPC envelope below the 16 MiB frame.
        const RPC_FRAME_BYTES: usize = 16 << 20;
        let encoded_bytes = MAX_TOTAL_IMAGE_BYTES.div_ceil(3) * 4;
        assert!(encoded_bytes + (1 << 20) < RPC_FRAME_BYTES);

        let png = valid_png();
        let mut attachments = ImageAttachments::new();
        attachments
            .push(ImageAttachment::from_bytes("one", png.clone()).unwrap())
            .unwrap();
        attachments
            .push(ImageAttachment::from_bytes("two", png.clone()).unwrap())
            .unwrap();
        assert_eq!(attachments.len(), 2);
        assert_eq!(attachments.total_bytes(), png.len() * 2);
        assert_eq!(attachments.rpc_content_blocks().len(), 2);
        let removed = attachments.remove(0).unwrap();
        assert_eq!(removed.label(), "one");
        assert_eq!(attachments.total_bytes(), png.len());
        attachments.clear();
        assert!(attachments.is_empty());
        assert_eq!(attachments.total_bytes(), 0);
    }

    #[test]
    fn chooses_preferred_available_linux_clipboard_type() {
        assert_eq!(
            preferred_clipboard_image_type("text/plain\nimage/gif\nimage/png\n"),
            Some("image/png")
        );
        assert_eq!(preferred_clipboard_image_type("text/plain"), None);
    }

    #[cfg(unix)]
    #[test]
    fn command_output_is_bounded_and_timed_out() {
        let output = bounded_command_output(
            Instant::now() + Duration::from_secs(1),
            4,
            "sh",
            &["-c", "printf test"],
        )
        .unwrap();
        assert_eq!(output, b"test");

        assert!(matches!(
            bounded_command_output(
                Instant::now() + Duration::from_secs(1),
                3,
                "sh",
                &["-c", "printf test"],
            ),
            Err(AttachmentError::CommandOutputTooLarge { .. })
        ));
        let started = Instant::now();
        assert!(matches!(
            bounded_command_output(
                Instant::now() + Duration::from_millis(20),
                4,
                "sh",
                &["-c", "sleep 1; printf test"],
            ),
            Err(AttachmentError::CommandTimedOut { .. })
        ));
        assert!(started.elapsed() < Duration::from_millis(500));
    }
}
