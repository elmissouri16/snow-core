use std::{
    env, fs,
    io::BufRead,
    path::{Path, PathBuf},
    process::{Child, ChildStderr, ChildStdin, ChildStdout, Command, Stdio},
    time::Duration,
};

use super::{SnowError, protocol::DEFAULT_MAX_FRAME_BYTES};

#[derive(Debug, Clone)]
pub struct RuntimeConfig {
    pub executable: PathBuf,
    pub project_root: PathBuf,
    pub provider: String,
    pub model: Option<String>,
    pub session_path: Option<PathBuf>,
    pub no_session: bool,
    pub startup_timeout: Duration,
    pub shutdown_timeout: Duration,
    pub max_frame_bytes: usize,
}

impl RuntimeConfig {
    pub fn from_env() -> Result<Self, SnowError> {
        let executable = match env::var_os("SNOW_BINARY") {
            Some(path) => PathBuf::from(path),
            None => {
                let local = PathBuf::from("../snow");
                if local.is_file() {
                    local
                } else {
                    PathBuf::from("snow")
                }
            }
        };
        let project_root = match env::var_os("SNOW_PROJECT") {
            Some(path) => PathBuf::from(path),
            None => env::current_dir().map_err(SnowError::io)?,
        };
        let provider = env::var("SNOW_PROVIDER").unwrap_or_else(|_| "fake".into());
        let model = env::var("SNOW_MODEL")
            .ok()
            .filter(|model| !model.trim().is_empty());
        let session_path = env::var_os("SNOW_SESSION")
            .filter(|path| !path.is_empty())
            .map(PathBuf::from);
        let no_session = env::var_os("SNOW_NO_SESSION").is_some_and(|value| value != "0");

        Ok(Self {
            executable,
            project_root,
            provider,
            model,
            session_path,
            no_session,
            startup_timeout: Duration::from_secs(10),
            shutdown_timeout: Duration::from_secs(3),
            max_frame_bytes: DEFAULT_MAX_FRAME_BYTES,
        })
    }
}

pub(crate) struct SpawnedProcess {
    pub child: Child,
    pub stdin: ChildStdin,
    pub stdout: ChildStdout,
    pub stderr: ChildStderr,
}

pub(crate) fn spawn(config: &RuntimeConfig) -> Result<SpawnedProcess, SnowError> {
    let executable = resolve_executable(&config.executable)?;
    if !config.project_root.is_dir() {
        return Err(SnowError::Protocol(format!(
            "project directory does not exist: {}",
            config.project_root.display()
        )));
    }
    if config.provider.trim().is_empty() {
        return Err(SnowError::Protocol("provider must not be empty".into()));
    }

    let mut command = Command::new(&executable);
    command.args([
        "--mode",
        "rpc",
        "--provider",
        config.provider.as_str(),
        "--permission",
        "ask",
        "--thinking",
        "off",
        "--no-plugins",
        "--no-mcp",
        "--no-skills",
        "--no-subagents",
    ]);
    if let Some(model) = config.model.as_deref() {
        command.args(["--model", model]);
    }
    if config.no_session {
        command.arg("--no-session");
    } else if let Some(path) = config.session_path.as_deref() {
        command.arg("--session").arg(path);
    }

    let mut child = command
        .current_dir(&config.project_root)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(SnowError::Spawn)?;

    let stdin = child
        .stdin
        .take()
        .ok_or_else(|| SnowError::Protocol("Snow stdin was not piped".into()))?;
    let stdout = child
        .stdout
        .take()
        .ok_or_else(|| SnowError::Protocol("Snow stdout was not piped".into()))?;
    let stderr = child
        .stderr
        .take()
        .ok_or_else(|| SnowError::Protocol("Snow stderr was not piped".into()))?;

    Ok(SpawnedProcess {
        child,
        stdin,
        stdout,
        stderr,
    })
}

fn resolve_executable(executable: &Path) -> Result<PathBuf, SnowError> {
    let has_path_separator = executable.components().count() > 1
        || executable
            .as_os_str()
            .to_string_lossy()
            .contains(std::path::MAIN_SEPARATOR);
    if !has_path_separator {
        return Ok(executable.to_path_buf());
    }

    let resolved = fs::canonicalize(executable)
        .map_err(|_| SnowError::ExecutableNotFound(executable.to_path_buf()))?;
    if !resolved.is_file() {
        return Err(SnowError::ExecutableNotFound(executable.to_path_buf()));
    }
    Ok(resolved)
}

pub(crate) fn read_bounded_frame<R: BufRead>(
    reader: &mut R,
    limit: usize,
) -> Result<Option<Vec<u8>>, SnowError> {
    let mut frame = Vec::new();
    loop {
        let available = reader.fill_buf().map_err(SnowError::io)?;
        if available.is_empty() {
            if frame.is_empty() {
                return Ok(None);
            }
            return Err(SnowError::Protocol(
                "Snow stdout ended in the middle of a JSONL frame".into(),
            ));
        }

        if let Some(newline) = available.iter().position(|byte| *byte == b'\n') {
            if frame.len().saturating_add(newline) > limit {
                return Err(SnowError::FrameTooLarge { limit });
            }
            frame.extend_from_slice(&available[..newline]);
            reader.consume(newline + 1);
            return Ok(Some(frame));
        }

        if frame.len().saturating_add(available.len()) > limit {
            return Err(SnowError::FrameTooLarge { limit });
        }
        let consumed = available.len();
        frame.extend_from_slice(available);
        reader.consume(consumed);
    }
}

#[cfg(test)]
mod tests {
    use std::io::{BufReader, Cursor};

    use super::*;

    #[test]
    fn relative_executable_is_resolved_before_child_working_directory_changes() {
        let current_dir = std::env::current_dir().unwrap();
        let current_exe = std::env::current_exe().unwrap();
        let relative = current_exe.strip_prefix(current_dir).unwrap();
        let resolved = resolve_executable(relative).unwrap();
        assert!(resolved.is_absolute());
        assert_eq!(resolved, current_exe.canonicalize().unwrap());
    }

    #[test]
    fn bounded_reader_handles_multiple_and_partial_frames() {
        let input = Cursor::new(b"one\ntwo\n".to_vec());
        let mut reader = BufReader::with_capacity(2, input);
        assert_eq!(
            read_bounded_frame(&mut reader, 16).unwrap(),
            Some(b"one".to_vec())
        );
        assert_eq!(
            read_bounded_frame(&mut reader, 16).unwrap(),
            Some(b"two".to_vec())
        );
        assert_eq!(read_bounded_frame(&mut reader, 16).unwrap(), None);
    }

    #[test]
    fn bounded_reader_allows_empty_lines() {
        let mut reader = BufReader::new(Cursor::new(b"\n".to_vec()));
        assert_eq!(
            read_bounded_frame(&mut reader, 16).unwrap(),
            Some(Vec::new())
        );
    }

    #[test]
    fn bounded_reader_rejects_oversized_frames() {
        let mut reader = BufReader::new(Cursor::new(b"12345\n".to_vec()));
        assert!(matches!(
            read_bounded_frame(&mut reader, 4),
            Err(SnowError::FrameTooLarge { limit: 4 })
        ));
    }

    #[test]
    fn bounded_reader_rejects_truncated_frames() {
        let mut reader = BufReader::new(Cursor::new(b"partial".to_vec()));
        assert!(matches!(
            read_bounded_frame(&mut reader, 16),
            Err(SnowError::Protocol(_))
        ));
    }
}
