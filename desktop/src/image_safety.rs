use std::{
    io::{self, Cursor, Seek, SeekFrom, Write},
    sync::Arc,
};

use gpui::{Image, ImageFormat as GpuiImageFormat};
use image::{GenericImageView, ImageFormat, ImageReader, Limits};

pub const MAX_IMAGE_INPUT_BYTES: usize = 8 * 1024 * 1024;
pub const MAX_IMAGE_DIMENSION: u32 = 8_192;
pub const MAX_IMAGE_PIXELS: u64 = 40_000_000;
pub const MAX_IMAGE_DECODE_BYTES: u64 = MAX_IMAGE_PIXELS * 4;
pub const MAX_IMAGE_PREVIEW_BYTES: usize = 64 * 1024 * 1024;

pub fn validate_and_cache_image(mime_type: &str, data: &[u8]) -> Result<Arc<Image>, String> {
    if data.is_empty() || data.len() > MAX_IMAGE_INPUT_BYTES {
        return Err(format!(
            "image input must contain 1..={MAX_IMAGE_INPUT_BYTES} bytes"
        ));
    }
    let expected = image_format_for_mime(mime_type)
        .ok_or_else(|| format!("unsupported image MIME type {mime_type:?}"))?;
    let detected = image::guess_format(data).map_err(|error| format!("invalid image: {error}"))?;
    if detected != expected {
        return Err(format!(
            "image content is {:?}, not the declared {mime_type}",
            detected
                .extensions_str()
                .first()
                .copied()
                .unwrap_or("unknown")
        ));
    }

    // Inspect dimensions without allocating the decoded pixel buffer. Pixel and
    // axis limits must be enforced before decoding so compressed images cannot
    // force a large allocation merely to be rejected afterwards.
    let (width, height) = ImageReader::with_format(Cursor::new(data), expected)
        .into_dimensions()
        .map_err(|error| format!("image dimensions could not be read safely: {error}"))?;
    let pixels = u64::from(width).saturating_mul(u64::from(height));
    if width == 0
        || height == 0
        || width > MAX_IMAGE_DIMENSION
        || height > MAX_IMAGE_DIMENSION
        || pixels > MAX_IMAGE_PIXELS
    {
        return Err(format!(
            "image dimensions {width}×{height} exceed the safe preview limit"
        ));
    }

    let mut reader = ImageReader::with_format(Cursor::new(data), expected);
    let mut limits = Limits::default();
    limits.max_image_width = Some(MAX_IMAGE_DIMENSION);
    limits.max_image_height = Some(MAX_IMAGE_DIMENSION);
    limits.max_alloc = Some(MAX_IMAGE_DECODE_BYTES);
    reader.limits(limits);
    let image = reader
        .decode()
        .map_err(|error| format!("image cannot be decoded safely: {error}"))?;
    debug_assert_eq!(image.dimensions(), (width, height));

    // Convert the validated first frame into one bounded, static PNG. GPUI never
    // receives the original animated/compressed payload, which avoids repeated
    // decoding and prevents animation/decompression bombs in the renderer.
    // The writer itself is capped so an incompressible preview cannot allocate
    // beyond the output budget before the size check runs.
    let mut preview = BoundedCursor::new(MAX_IMAGE_PREVIEW_BYTES);
    image
        .write_to(&mut preview, ImageFormat::Png)
        .map_err(|error| format!("image preview could not be encoded safely: {error}"))?;
    Ok(Arc::new(Image::from_bytes(
        GpuiImageFormat::Png,
        preview.into_inner(),
    )))
}

struct BoundedCursor {
    inner: Cursor<Vec<u8>>,
    limit: usize,
}

impl BoundedCursor {
    fn new(limit: usize) -> Self {
        Self {
            inner: Cursor::new(Vec::new()),
            limit,
        }
    }

    fn into_inner(self) -> Vec<u8> {
        self.inner.into_inner()
    }
}

impl Write for BoundedCursor {
    fn write(&mut self, buffer: &[u8]) -> io::Result<usize> {
        let end = self
            .inner
            .position()
            .checked_add(buffer.len() as u64)
            .ok_or_else(|| io::Error::other("image preview position overflow"))?;
        if end > self.limit as u64 {
            return Err(io::Error::other("image preview exceeds output limit"));
        }
        self.inner.write(buffer)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.inner.flush()
    }
}

impl Seek for BoundedCursor {
    fn seek(&mut self, position: SeekFrom) -> io::Result<u64> {
        let original = self.inner.position();
        let next = self.inner.seek(position)?;
        if next > self.limit as u64 {
            self.inner.set_position(original);
            return Err(io::Error::other("image preview seek exceeds output limit"));
        }
        Ok(next)
    }
}

fn image_format_for_mime(mime_type: &str) -> Option<ImageFormat> {
    match mime_type {
        "image/png" => Some(ImageFormat::Png),
        "image/jpeg" => Some(ImageFormat::Jpeg),
        "image/gif" => Some(ImageFormat::Gif),
        "image/webp" => Some(ImageFormat::WebP),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn one_pixel_png() -> Vec<u8> {
        let mut bytes = Cursor::new(Vec::new());
        image::DynamicImage::new_rgba8(1, 1)
            .write_to(&mut bytes, ImageFormat::Png)
            .unwrap();
        bytes.into_inner()
    }

    fn png_with_declared_dimensions(width: u32, height: u32) -> Vec<u8> {
        let mut png = one_pixel_png();
        png[16..20].copy_from_slice(&width.to_be_bytes());
        png[20..24].copy_from_slice(&height.to_be_bytes());
        let checksum = crc32(&png[12..29]);
        png[29..33].copy_from_slice(&checksum.to_be_bytes());
        png
    }

    fn crc32(bytes: &[u8]) -> u32 {
        let mut crc = u32::MAX;
        for byte in bytes {
            crc ^= u32::from(*byte);
            for _ in 0..8 {
                crc = (crc >> 1) ^ (0xedb8_8320 & (0u32.wrapping_sub(crc & 1)));
            }
        }
        !crc
    }

    #[test]
    fn validates_and_caches_a_bounded_static_png_preview() {
        let source = one_pixel_png();
        let preview = validate_and_cache_image("image/png", &source).unwrap();
        assert_eq!(preview.format, GpuiImageFormat::Png);
        assert!(!preview.bytes.is_empty());
        assert!(preview.bytes.len() <= MAX_IMAGE_PREVIEW_BYTES);
        assert_eq!(
            image::load_from_memory(preview.bytes.as_ref())
                .unwrap()
                .dimensions(),
            (1, 1)
        );

        let cached = Arc::clone(&preview);
        assert!(Arc::ptr_eq(&preview, &cached));
    }

    #[test]
    fn rejects_declared_mime_mismatch_corrupt_data_and_unbounded_input() {
        let source = one_pixel_png();
        let mismatch = validate_and_cache_image("image/jpeg", &source).unwrap_err();
        assert!(mismatch.contains("not the declared"));
        assert!(validate_and_cache_image("image/png", b"not an image").is_err());
        assert!(validate_and_cache_image("image/png", &[]).is_err());
        assert!(
            validate_and_cache_image("image/png", &vec![0; MAX_IMAGE_INPUT_BYTES + 1]).is_err()
        );
    }

    #[test]
    fn rejects_excessive_axis_and_pixel_counts_before_decoding() {
        let excessive_axis = png_with_declared_dimensions(MAX_IMAGE_DIMENSION + 1, 1);
        let error = validate_and_cache_image("image/png", &excessive_axis).unwrap_err();
        assert!(error.contains("dimensions"));

        let excessive_pixels =
            png_with_declared_dimensions(MAX_IMAGE_DIMENSION, MAX_IMAGE_DIMENSION);
        let error = validate_and_cache_image("image/png", &excessive_pixels).unwrap_err();
        assert!(error.contains("dimensions"));
    }

    #[test]
    fn bounded_preview_writer_rejects_growth_and_seek_past_limit() {
        let mut writer = BoundedCursor::new(4);
        assert_eq!(writer.write(b"test").unwrap(), 4);
        assert!(writer.write(b"x").is_err());
        assert!(writer.seek(SeekFrom::Start(5)).is_err());
        assert_eq!(writer.into_inner(), b"test");
    }
}
