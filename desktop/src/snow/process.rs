use std::{
    env,
    ffi::OsString,
    fs,
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
    /// Explicit provider override; empty preserves Snow's canonical config selection.
    pub provider: String,
    pub model: Option<String>,
    pub permission: Option<String>,
    pub thinking: Option<String>,
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
        let provider = parse_provider_override(env::var_os("SNOW_PROVIDER"))?;
        let model = env::var("SNOW_MODEL")
            .ok()
            .filter(|model| !model.trim().is_empty());
        let permission = parse_optional_override(
            "SNOW_PERMISSION",
            env::var_os("SNOW_PERMISSION"),
            &["ask", "allow", "deny"],
        )?;
        let thinking = parse_optional_override(
            "SNOW_THINKING",
            env::var_os("SNOW_THINKING"),
            &[
                "off", "minimal", "low", "medium", "high", "xhigh", "max", "ultra",
            ],
        )?;
        let session_path = env::var_os("SNOW_SESSION")
            .filter(|path| !path.is_empty())
            .map(PathBuf::from);
        let no_session = env::var_os("SNOW_NO_SESSION").is_some_and(|value| value != "0");

        Ok(Self {
            executable,
            project_root,
            provider,
            model,
            permission,
            thinking,
            session_path,
            no_session,
            startup_timeout: Duration::from_secs(10),
            shutdown_timeout: Duration::from_secs(3),
            max_frame_bytes: DEFAULT_MAX_FRAME_BYTES,
        })
    }
}

fn parse_provider_override(value: Option<OsString>) -> Result<String, SnowError> {
    let Some(value) = value else {
        return Ok(String::new());
    };
    let value = value
        .into_string()
        .map_err(|_| SnowError::Protocol("SNOW_PROVIDER must be valid UTF-8".into()))?;
    validate_provider_override(&value)?;
    Ok(value)
}

fn validate_provider_override(value: &str) -> Result<(), SnowError> {
    if value.is_empty() || value.len() > 64 {
        return Err(SnowError::Protocol(
            "SNOW_PROVIDER must be 1..64 characters".into(),
        ));
    }
    for (index, character) in value.chars().enumerate() {
        let valid = character.is_ascii_lowercase()
            || character.is_ascii_digit()
            || index > 0 && matches!(character, '-' | '_' | '.');
        if !valid {
            return Err(SnowError::Protocol(
                "SNOW_PROVIDER must use lowercase letters, digits, and internal ._- characters"
                    .into(),
            ));
        }
    }
    Ok(())
}

fn parse_optional_override(
    name: &str,
    value: Option<OsString>,
    allowed: &[&str],
) -> Result<Option<String>, SnowError> {
    let Some(value) = value else {
        return Ok(None);
    };
    let value = value
        .into_string()
        .map_err(|_| SnowError::Protocol(format!("{name} must be valid UTF-8")))?;
    validate_override(name, &value, allowed)?;
    Ok(Some(value))
}

fn validate_override(name: &str, value: &str, allowed: &[&str]) -> Result<(), SnowError> {
    if allowed.contains(&value) {
        return Ok(());
    }
    Err(SnowError::Protocol(format!(
        "{name} must be one of {}",
        allowed.join("|")
    )))
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
    let args = launch_args(config)?;
    let mut command = Command::new(&executable);
    command.args(args);

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

fn launch_args(config: &RuntimeConfig) -> Result<Vec<OsString>, SnowError> {
    let mut args = vec![OsString::from("--mode"), OsString::from("rpc")];
    if !config.provider.is_empty() {
        validate_provider_override(&config.provider)?;
        args.push(OsString::from("--provider"));
        args.push(OsString::from(&config.provider));
    }
    if let Some(permission) = config.permission.as_deref() {
        validate_override("permission", permission, &["ask", "allow", "deny"])?;
        args.push(OsString::from("--permission"));
        args.push(OsString::from(permission));
    }
    if let Some(thinking) = config.thinking.as_deref() {
        validate_override(
            "thinking",
            thinking,
            &[
                "off", "minimal", "low", "medium", "high", "xhigh", "max", "ultra",
            ],
        )?;
        args.push(OsString::from("--thinking"));
        args.push(OsString::from(thinking));
    }
    if let Some(model) = config.model.as_deref() {
        args.push(OsString::from("--model"));
        args.push(OsString::from(model));
    }
    if config.no_session {
        args.push(OsString::from("--no-session"));
    } else if let Some(path) = config.session_path.as_deref() {
        args.push(OsString::from("--session"));
        args.push(path.as_os_str().to_owned());
    }
    Ok(args)
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

    fn launch_config(provider: &str) -> RuntimeConfig {
        RuntimeConfig {
            executable: PathBuf::from("snow"),
            project_root: PathBuf::from("."),
            provider: provider.into(),
            model: None,
            permission: None,
            thinking: None,
            session_path: None,
            no_session: false,
            startup_timeout: Duration::from_secs(10),
            shutdown_timeout: Duration::from_secs(3),
            max_frame_bytes: DEFAULT_MAX_FRAME_BYTES,
        }
    }

    #[test]
    fn provider_override_is_optional_and_validated() {
        assert_eq!(parse_provider_override(None).unwrap(), "");
        assert_eq!(
            parse_provider_override(Some(OsString::from("fake"))).unwrap(),
            "fake"
        );
        assert_eq!(
            parse_provider_override(Some(OsString::from("team.gateway_2"))).unwrap(),
            "team.gateway_2"
        );

        for invalid in ["", "Fake", "-profile", "profile/one"] {
            assert!(parse_provider_override(Some(OsString::from(invalid))).is_err());
        }
        assert!(parse_provider_override(Some(OsString::from("a".repeat(65)))).is_err());
    }

    #[test]
    fn launch_args_do_not_override_provider_when_unset() {
        let args = launch_args(&launch_config("")).unwrap();
        assert_eq!(args, [OsString::from("--mode"), OsString::from("rpc")]);
        assert!(!args.iter().any(|arg| arg == "--provider"));
    }

    #[test]
    fn launch_args_include_only_explicit_validated_overrides() {
        let mut config = launch_config("fake");
        config.model = Some("fixture-model".into());
        config.permission = Some("deny".into());
        config.thinking = Some("off".into());
        config.no_session = true;

        assert_eq!(
            launch_args(&config).unwrap(),
            [
                "--mode",
                "rpc",
                "--provider",
                "fake",
                "--permission",
                "deny",
                "--thinking",
                "off",
                "--model",
                "fixture-model",
                "--no-session",
            ]
            .map(OsString::from)
        );

        config.provider = "Fake".into();
        assert!(launch_args(&config).is_err());
    }

    #[test]
    fn optional_launch_overrides_are_strict_and_exact() {
        assert_eq!(
            parse_optional_override("SNOW_PERMISSION", None, &["ask", "allow", "deny"]).unwrap(),
            None
        );
        assert_eq!(
            parse_optional_override(
                "SNOW_THINKING",
                Some(OsString::from("xhigh")),
                &["off", "low", "xhigh"]
            )
            .unwrap(),
            Some("xhigh".into())
        );

        for invalid in ["", "ASK", "always"] {
            let error = parse_optional_override(
                "SNOW_PERMISSION",
                Some(OsString::from(invalid)),
                &["ask", "allow", "deny"],
            )
            .unwrap_err();
            assert!(error.to_string().contains("SNOW_PERMISSION must be one of"));
        }
    }

    #[test]
    fn runtime_override_validation_rejects_programmatic_invalid_values() {
        assert!(validate_override("permission", "ask", &["ask", "allow", "deny"]).is_ok());
        assert!(validate_override("thinking", "ultra", &["off", "ultra"]).is_ok());
        assert!(validate_override("thinking", "extreme", &["off", "ultra"]).is_err());
    }

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
