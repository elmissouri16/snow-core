//! Versioned, bounded desktop-only preferences.
//!
//! The desktop client deliberately keeps these presentation preferences
//! separate from Snow's canonical runtime settings. Reads and writes are
//! bounded and reject special files so loading preferences cannot block on a
//! device or FIFO. On Unix, replacement files are created with mode `0600`.

use std::{
    collections::BTreeMap,
    env,
    fs::{self, File, OpenOptions},
    io::{self, Read, Write},
    path::{Path, PathBuf},
    sync::atomic::{AtomicU64, Ordering},
};

use serde::{Deserialize, Deserializer, Serialize};
use thiserror::Error;

use crate::keybindings::{ACTION_INVENTORY, CanonicalShortcut};

pub const PREFERENCES_VERSION: u32 = 1;
pub const MAX_PREFERENCES_BYTES: u64 = 64 << 10;
/// Maximum number of semantic actions that may have explicit overrides.
pub const MAX_BINDING_OVERRIDES: usize = 64;
/// Maximum total shortcuts across every action override.
pub const MAX_BINDING_KEYS: usize = 256;
/// Maximum encoded byte length of one portable shortcut.
pub const MAX_KEY_BYTES: usize = 128;

static TEMP_SEQUENCE: AtomicU64 = AtomicU64::new(0);

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Appearance {
    #[default]
    System,
    Light,
    Dark,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct DesktopPreferences {
    pub version: u32,
    #[serde(default)]
    pub appearance: Appearance,
    /// Semantic action overrides in deterministic action-name order.
    ///
    /// Multiple shortcuts may dispatch one action. An empty array explicitly
    /// unbinds it. Version-one readers also accept one legacy string per action,
    /// while serialization always emits arrays.
    #[serde(default, deserialize_with = "deserialize_bindings")]
    pub bindings: BTreeMap<String, Vec<String>>,
}

impl Default for DesktopPreferences {
    fn default() -> Self {
        Self {
            version: PREFERENCES_VERSION,
            appearance: Appearance::System,
            bindings: BTreeMap::new(),
        }
    }
}

impl DesktopPreferences {
    pub fn validate(&self) -> Result<(), PreferencesError> {
        if self.version != PREFERENCES_VERSION {
            return Err(PreferencesError::UnsupportedVersion(self.version));
        }
        if self.bindings.len() > MAX_BINDING_OVERRIDES {
            return Err(PreferencesError::TooManyBindings {
                count: self.bindings.len(),
                limit: MAX_BINDING_OVERRIDES,
            });
        }
        let total_keys = self
            .bindings
            .values()
            .try_fold(0_usize, |total, keys| total.checked_add(keys.len()))
            .unwrap_or(usize::MAX);
        if total_keys > MAX_BINDING_KEYS {
            return Err(PreferencesError::TooManyKeys {
                count: total_keys,
                limit: MAX_BINDING_KEYS,
            });
        }
        for (action, keys) in &self.bindings {
            if !ACTION_INVENTORY
                .iter()
                .any(|definition| definition.name == action)
            {
                return Err(PreferencesError::UnknownAction(action.clone()));
            }
            let mut canonical_keys = std::collections::BTreeSet::new();
            for key in keys {
                let canonical = validate_key(action, key)?;
                if !canonical_keys.insert(canonical.as_str().to_owned()) {
                    return Err(PreferencesError::DuplicateKey {
                        action: action.clone(),
                        key: canonical.as_str().to_owned(),
                    });
                }
            }
        }
        Ok(())
    }
}

#[derive(Debug, Deserialize)]
#[serde(untagged)]
enum BindingValue {
    Legacy(String),
    Multiple(Vec<String>),
}

fn deserialize_bindings<'de, D>(deserializer: D) -> Result<BTreeMap<String, Vec<String>>, D::Error>
where
    D: Deserializer<'de>,
{
    BTreeMap::<String, BindingValue>::deserialize(deserializer).map(|bindings| {
        bindings
            .into_iter()
            .map(|(action, value)| {
                let keys = match value {
                    BindingValue::Legacy(key) => vec![key],
                    BindingValue::Multiple(keys) => keys,
                };
                (action, keys)
            })
            .collect()
    })
}

#[derive(Debug, Error)]
pub enum PreferencesError {
    #[error("neither SNOW_HOME nor HOME identifies a preferences directory")]
    MissingHome,
    #[error("preferences path has no parent directory: {0}")]
    MissingParent(PathBuf),
    #[error("preferences path is a symbolic link: {0}")]
    Symlink(PathBuf),
    #[error("preferences path is not a regular file: {0}")]
    NotRegularFile(PathBuf),
    #[error("preferences parent is not a real directory: {0}")]
    InvalidParent(PathBuf),
    #[error("preferences exceed the {limit} byte limit")]
    TooLarge { limit: u64 },
    #[error("preferences use unsupported version {0}")]
    UnsupportedVersion(u32),
    #[error("unknown keybinding action {0:?}")]
    UnknownAction(String),
    #[error("preferences contain {count} binding actions; at most {limit} are allowed")]
    TooManyBindings { count: usize, limit: usize },
    #[error("preferences contain {count} keys; at most {limit} are allowed")]
    TooManyKeys { count: usize, limit: usize },
    #[error("duplicate key {key:?} for action {action:?}")]
    DuplicateKey { action: String, key: String },
    #[error("invalid keybinding for {action:?}: {reason}")]
    InvalidKey {
        action: String,
        reason: &'static str,
    },
    #[error("could not decode preferences: {0}")]
    Decode(#[from] serde_json::Error),
    #[error("preferences I/O failed for {path}: {source}")]
    Io {
        path: PathBuf,
        #[source]
        source: io::Error,
    },
}

/// Resolve the desktop preferences path without reading or creating it.
pub fn preferences_path() -> Result<PathBuf, PreferencesError> {
    preferences_path_from(
        env::var_os("SNOW_HOME").as_deref(),
        env::var_os("HOME").as_deref(),
    )
}

fn preferences_path_from(
    snow_home: Option<&std::ffi::OsStr>,
    home: Option<&std::ffi::OsStr>,
) -> Result<PathBuf, PreferencesError> {
    if let Some(snow_home) = snow_home.filter(|value| !value.is_empty()) {
        return Ok(PathBuf::from(snow_home).join("desktop.json"));
    }
    let home = home
        .filter(|value| !value.is_empty())
        .ok_or(PreferencesError::MissingHome)?;
    Ok(PathBuf::from(home).join(".snow").join("desktop.json"))
}

/// Load preferences from the resolved Snow home. A missing file is equivalent
/// to the default version-one preferences.
pub fn load() -> Result<DesktopPreferences, PreferencesError> {
    load_from_path(&preferences_path()?)
}

/// Save preferences to the resolved Snow home using atomic replacement.
pub fn save(preferences: &DesktopPreferences) -> Result<(), PreferencesError> {
    save_to_path(&preferences_path()?, preferences)
}

/// Load preferences from an explicit path.
///
/// This API is also useful to embedders and keeps tests independent of process
/// environment mutation. The file identity is checked before and after open to
/// detect replacement races; special files and symlinks are rejected.
pub fn load_from_path(path: &Path) -> Result<DesktopPreferences, PreferencesError> {
    let before = match fs::symlink_metadata(path) {
        Ok(metadata) => metadata,
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            return Ok(DesktopPreferences::default());
        }
        Err(source) => return Err(io_error(path, source)),
    };
    validate_regular_metadata(path, &before)?;
    if before.len() > MAX_PREFERENCES_BYTES {
        return Err(PreferencesError::TooLarge {
            limit: MAX_PREFERENCES_BYTES,
        });
    }

    let mut file = open_input(path)?;
    let opened = file.metadata().map_err(|source| io_error(path, source))?;
    validate_regular_metadata(path, &opened)?;
    ensure_same_file(path, &before, &opened)?;

    let after = fs::symlink_metadata(path).map_err(|source| io_error(path, source))?;
    validate_regular_metadata(path, &after)?;
    ensure_same_file(path, &opened, &after)?;

    let mut bytes = Vec::with_capacity((opened.len() as usize).min(MAX_PREFERENCES_BYTES as usize));
    Read::by_ref(&mut file)
        .take(MAX_PREFERENCES_BYTES + 1)
        .read_to_end(&mut bytes)
        .map_err(|source| io_error(path, source))?;
    if bytes.len() as u64 > MAX_PREFERENCES_BYTES {
        return Err(PreferencesError::TooLarge {
            limit: MAX_PREFERENCES_BYTES,
        });
    }

    let preferences: DesktopPreferences = serde_json::from_slice(&bytes)?;
    preferences.validate()?;
    Ok(preferences)
}

/// Validate and atomically replace an explicit preferences file.
///
/// Serialization and validation happen before a temporary file is created. A
/// failure before `rename` therefore leaves any previous preferences intact.
/// The temporary file is removed on every pre-rename error.
pub fn save_to_path(path: &Path, preferences: &DesktopPreferences) -> Result<(), PreferencesError> {
    preferences.validate()?;
    let mut bytes = serde_json::to_vec_pretty(preferences)?;
    bytes.push(b'\n');
    if bytes.len() as u64 > MAX_PREFERENCES_BYTES {
        return Err(PreferencesError::TooLarge {
            limit: MAX_PREFERENCES_BYTES,
        });
    }

    let parent = path
        .parent()
        .filter(|parent| !parent.as_os_str().is_empty())
        .ok_or_else(|| PreferencesError::MissingParent(path.to_path_buf()))?;
    ensure_parent_directory(parent)?;
    validate_existing_destination(path)?;

    let (temporary_path, mut temporary) = create_temporary(parent, path)?;
    let mut guard = TemporaryGuard::new(temporary_path.clone());
    temporary
        .write_all(&bytes)
        .map_err(|source| io_error(&temporary_path, source))?;
    temporary
        .sync_all()
        .map_err(|source| io_error(&temporary_path, source))?;
    drop(temporary);

    // Recheck immediately before replacement. `rename` replaces the directory
    // entry itself and therefore never follows an existing destination link.
    validate_existing_destination(path)?;
    fs::rename(&temporary_path, path).map_err(|source| io_error(path, source))?;
    guard.disarm();

    File::open(parent)
        .and_then(|directory| directory.sync_all())
        .map_err(|source| io_error(parent, source))?;
    Ok(())
}

fn validate_key(action: &str, key: &str) -> Result<CanonicalShortcut, PreferencesError> {
    if key.len() > MAX_KEY_BYTES {
        return Err(PreferencesError::InvalidKey {
            action: action.to_owned(),
            reason: "key is too long",
        });
    }
    if key.trim() != key {
        return Err(PreferencesError::InvalidKey {
            action: action.to_owned(),
            reason: "leading or trailing whitespace is not allowed",
        });
    }
    CanonicalShortcut::parse(key).map_err(|error| PreferencesError::InvalidKey {
        action: action.to_owned(),
        reason: error.reason,
    })
}

fn open_input(path: &Path) -> Result<File, PreferencesError> {
    let mut options = OpenOptions::new();
    options.read(true);
    // The supported desktop targets are macOS and Linux. Nonblocking prevents
    // a raced-in FIFO from hanging startup; no-follow prevents link traversal.
    #[cfg(target_os = "linux")]
    {
        use std::os::unix::fs::OpenOptionsExt as _;
        const O_NONBLOCK: i32 = 0x800;
        const O_NOFOLLOW: i32 = 0x20_000;
        options.custom_flags(O_NONBLOCK | O_NOFOLLOW);
    }
    #[cfg(target_os = "macos")]
    {
        use std::os::unix::fs::OpenOptionsExt as _;
        const O_NONBLOCK: i32 = 0x0004;
        const O_NOFOLLOW: i32 = 0x0100;
        options.custom_flags(O_NONBLOCK | O_NOFOLLOW);
    }
    options.open(path).map_err(|source| io_error(path, source))
}

fn validate_regular_metadata(path: &Path, metadata: &fs::Metadata) -> Result<(), PreferencesError> {
    if metadata.file_type().is_symlink() {
        return Err(PreferencesError::Symlink(path.to_path_buf()));
    }
    if !metadata.file_type().is_file() {
        return Err(PreferencesError::NotRegularFile(path.to_path_buf()));
    }
    Ok(())
}

#[cfg(unix)]
fn ensure_same_file(
    path: &Path,
    expected: &fs::Metadata,
    actual: &fs::Metadata,
) -> Result<(), PreferencesError> {
    use std::os::unix::fs::MetadataExt as _;

    if expected.dev() != actual.dev() || expected.ino() != actual.ino() {
        return Err(PreferencesError::Io {
            path: path.to_path_buf(),
            source: io::Error::other("preferences changed while being opened"),
        });
    }
    Ok(())
}

#[cfg(not(unix))]
fn ensure_same_file(
    path: &Path,
    expected: &fs::Metadata,
    actual: &fs::Metadata,
) -> Result<(), PreferencesError> {
    if expected.len() != actual.len() || expected.modified().ok() != actual.modified().ok() {
        return Err(PreferencesError::Io {
            path: path.to_path_buf(),
            source: io::Error::other("preferences changed while being opened"),
        });
    }
    Ok(())
}

fn ensure_parent_directory(parent: &Path) -> Result<(), PreferencesError> {
    match fs::symlink_metadata(parent) {
        Ok(metadata) => {
            if metadata.file_type().is_symlink() || !metadata.is_dir() {
                return Err(PreferencesError::InvalidParent(parent.to_path_buf()));
            }
        }
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            fs::create_dir_all(parent).map_err(|source| io_error(parent, source))?;
            let metadata =
                fs::symlink_metadata(parent).map_err(|source| io_error(parent, source))?;
            if metadata.file_type().is_symlink() || !metadata.is_dir() {
                return Err(PreferencesError::InvalidParent(parent.to_path_buf()));
            }
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt as _;
                fs::set_permissions(parent, fs::Permissions::from_mode(0o700))
                    .map_err(|source| io_error(parent, source))?;
            }
        }
        Err(source) => return Err(io_error(parent, source)),
    }
    Ok(())
}

fn validate_existing_destination(path: &Path) -> Result<(), PreferencesError> {
    match fs::symlink_metadata(path) {
        Ok(metadata) => validate_regular_metadata(path, &metadata),
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(()),
        Err(source) => Err(io_error(path, source)),
    }
}

fn create_temporary(
    parent: &Path,
    destination: &Path,
) -> Result<(PathBuf, File), PreferencesError> {
    let stem = destination
        .file_name()
        .and_then(|name| name.to_str())
        .unwrap_or("desktop.json");
    for _ in 0..128 {
        let sequence = TEMP_SEQUENCE.fetch_add(1, Ordering::Relaxed);
        let path = parent.join(format!(".{stem}.tmp-{}-{sequence}", std::process::id()));
        let mut options = OpenOptions::new();
        options.write(true).create_new(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt as _;
            options.mode(0o600);
        }
        match options.open(&path) {
            Ok(file) => {
                #[cfg(unix)]
                {
                    use std::os::unix::fs::PermissionsExt as _;
                    if let Err(source) = file.set_permissions(fs::Permissions::from_mode(0o600)) {
                        drop(file);
                        let _ = fs::remove_file(&path);
                        return Err(io_error(&path, source));
                    }
                }
                return Ok((path, file));
            }
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(source) => return Err(io_error(&path, source)),
        }
    }
    Err(io_error(
        parent,
        io::Error::new(
            io::ErrorKind::AlreadyExists,
            "could not allocate a unique preferences temporary file",
        ),
    ))
}

fn io_error(path: &Path, source: io::Error) -> PreferencesError {
    PreferencesError::Io {
        path: path.to_path_buf(),
        source,
    }
}

struct TemporaryGuard {
    path: Option<PathBuf>,
}

impl TemporaryGuard {
    fn new(path: PathBuf) -> Self {
        Self { path: Some(path) }
    }

    fn disarm(&mut self) {
        self.path = None;
    }
}

impl Drop for TemporaryGuard {
    fn drop(&mut self) {
        if let Some(path) = self.path.take() {
            let _ = fs::remove_file(path);
        }
    }
}

#[cfg(test)]
mod tests {
    use std::{
        ffi::OsStr,
        fs,
        path::{Path, PathBuf},
        sync::atomic::{AtomicU64, Ordering},
    };

    use super::*;

    static TEST_SEQUENCE: AtomicU64 = AtomicU64::new(0);

    struct TestDirectory(PathBuf);

    impl TestDirectory {
        fn new(name: &str) -> Self {
            let sequence = TEST_SEQUENCE.fetch_add(1, Ordering::Relaxed);
            let path = env::temp_dir().join(format!(
                "snow-desktop-preferences-{name}-{}-{sequence}",
                std::process::id()
            ));
            if path.exists() {
                fs::remove_dir_all(&path).unwrap();
            }
            fs::create_dir(&path).unwrap();
            Self(path)
        }

        fn path(&self) -> &Path {
            &self.0
        }
    }

    impl Drop for TestDirectory {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    fn valid_preferences() -> DesktopPreferences {
        DesktopPreferences {
            appearance: Appearance::Dark,
            bindings: BTreeMap::from([
                ("line_up".into(), vec!["ctrl+up".into()]),
                ("picker_down".into(), vec!["down".into(), "j".into()]),
            ]),
            ..DesktopPreferences::default()
        }
    }

    #[test]
    fn resolves_snow_home_before_home() {
        assert_eq!(
            preferences_path_from(Some(OsStr::new("/snow")), Some(OsStr::new("/home/user")))
                .unwrap(),
            PathBuf::from("/snow/desktop.json")
        );
        assert_eq!(
            preferences_path_from(None, Some(OsStr::new("/home/user"))).unwrap(),
            PathBuf::from("/home/user/.snow/desktop.json")
        );
        assert!(matches!(
            preferences_path_from(None, None),
            Err(PreferencesError::MissingHome)
        ));
    }

    #[test]
    fn missing_file_loads_defaults() {
        let directory = TestDirectory::new("missing");
        assert_eq!(
            load_from_path(&directory.path().join("desktop.json")).unwrap(),
            DesktopPreferences::default()
        );
    }

    #[test]
    fn round_trip_is_deterministic_bounded_and_uses_canonical_arrays() {
        let directory = TestDirectory::new("round-trip");
        let path = directory.path().join("desktop.json");
        let preferences = valid_preferences();

        save_to_path(&path, &preferences).unwrap();
        let first = fs::read(&path).unwrap();
        assert!(first.len() <= MAX_PREFERENCES_BYTES as usize);
        let encoded: serde_json::Value = serde_json::from_slice(&first).unwrap();
        assert_eq!(
            encoded["bindings"]["line_up"],
            serde_json::json!(["ctrl+up"])
        );
        assert_eq!(
            encoded["bindings"]["picker_down"],
            serde_json::json!(["down", "j"])
        );
        assert_eq!(load_from_path(&path).unwrap(), preferences);
        save_to_path(&path, &preferences).unwrap();
        assert_eq!(fs::read(path).unwrap(), first);
    }

    #[test]
    fn legacy_single_strings_load_safely_and_save_as_arrays() {
        let directory = TestDirectory::new("legacy-binding");
        let path = directory.path().join("desktop.json");
        fs::write(&path, r#"{"version":1,"bindings":{"line_up":"ctrl+up"}}"#).unwrap();

        let preferences = load_from_path(&path).unwrap();
        assert_eq!(
            preferences.bindings["line_up"],
            vec![String::from("ctrl+up")]
        );
        save_to_path(&path, &preferences).unwrap();
        let encoded: serde_json::Value = serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
        assert_eq!(
            encoded["bindings"]["line_up"],
            serde_json::json!(["ctrl+up"])
        );
    }

    #[test]
    fn every_inventory_action_and_explicit_unbinding_is_accepted() {
        let preferences = DesktopPreferences {
            bindings: ACTION_INVENTORY
                .iter()
                .map(|definition| {
                    (
                        definition.name.to_owned(),
                        definition
                            .defaults
                            .iter()
                            .map(|key| (*key).to_owned())
                            .collect(),
                    )
                })
                .collect(),
            ..DesktopPreferences::default()
        };
        preferences.validate().unwrap();

        let unbound = DesktopPreferences {
            bindings: BTreeMap::from([("models".into(), Vec::new())]),
            ..DesktopPreferences::default()
        };
        unbound.validate().unwrap();
        let encoded = serde_json::to_value(&unbound).unwrap();
        assert_eq!(encoded["bindings"]["models"], serde_json::json!([]));
    }

    #[test]
    fn save_creates_the_preferences_parent() {
        let directory = TestDirectory::new("create-parent");
        let parent = directory.path().join(".snow");
        let path = parent.join("desktop.json");

        save_to_path(&path, &DesktopPreferences::default()).unwrap();
        assert!(parent.is_dir());
        assert_eq!(
            load_from_path(&path).unwrap(),
            DesktopPreferences::default()
        );

        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt as _;
            assert_eq!(
                fs::metadata(parent).unwrap().permissions().mode() & 0o777,
                0o700
            );
        }
    }

    #[test]
    fn rejects_unknown_fields_versions_actions_and_bad_keys() {
        let directory = TestDirectory::new("validation");
        let path = directory.path().join("desktop.json");

        for invalid in [
            r#"{"version":1,"unknown":true}"#,
            r#"{"version":2}"#,
            r#"{"version":1,"bindings":{"not_an_action":"x"}}"#,
            r#"{"version":1,"bindings":{"picker_up":""}}"#,
            r#"{"version":1,"bindings":{"picker_up":" down"}}"#,
            r#"{"version":1,"bindings":{"picker_up":["ctrl-up"]}}"#,
            r#"{"version":1,"bindings":{"abort":["esc","escape"]}}"#,
        ] {
            fs::write(&path, invalid).unwrap();
            assert!(load_from_path(&path).is_err(), "accepted {invalid}");
        }

        let mut preferences = DesktopPreferences::default();
        preferences.bindings.insert(
            "picker_up".into(),
            vec!["x".repeat(MAX_KEY_BYTES.saturating_add(1))],
        );
        assert!(matches!(
            preferences.validate(),
            Err(PreferencesError::InvalidKey { .. })
        ));

        let duplicate = DesktopPreferences {
            bindings: BTreeMap::from([("abort".into(), vec!["esc".into(), "escape".into()])]),
            ..DesktopPreferences::default()
        };
        assert!(matches!(
            duplicate.validate(),
            Err(PreferencesError::DuplicateKey { .. })
        ));

        let too_many_keys = DesktopPreferences {
            bindings: BTreeMap::from([(
                "picker_up".into(),
                vec!["x".into(); MAX_BINDING_KEYS + 1],
            )]),
            ..DesktopPreferences::default()
        };
        assert!(matches!(
            too_many_keys.validate(),
            Err(PreferencesError::TooManyKeys { .. })
        ));

        let too_many_actions = DesktopPreferences {
            bindings: (0..=MAX_BINDING_OVERRIDES)
                .map(|index| (format!("action-{index}"), Vec::new()))
                .collect(),
            ..DesktopPreferences::default()
        };
        assert!(matches!(
            too_many_actions.validate(),
            Err(PreferencesError::TooManyBindings { .. })
        ));
    }

    #[test]
    fn rejects_oversized_and_non_regular_inputs() {
        let directory = TestDirectory::new("special-files");
        let path = directory.path().join("desktop.json");
        fs::write(&path, vec![b' '; MAX_PREFERENCES_BYTES as usize + 1]).unwrap();
        assert!(matches!(
            load_from_path(&path),
            Err(PreferencesError::TooLarge { .. })
        ));

        fs::remove_file(&path).unwrap();
        fs::create_dir(&path).unwrap();
        assert!(matches!(
            load_from_path(&path),
            Err(PreferencesError::NotRegularFile(_))
        ));
    }

    #[cfg(unix)]
    #[test]
    fn rejects_symlinks_and_writes_mode_0600() {
        use std::os::unix::fs::{PermissionsExt as _, symlink};

        let directory = TestDirectory::new("unix-security");
        let target = directory.path().join("target.json");
        let link = directory.path().join("desktop.json");
        fs::write(&target, r#"{"version":1}"#).unwrap();
        symlink(&target, &link).unwrap();
        assert!(matches!(
            load_from_path(&link),
            Err(PreferencesError::Symlink(_))
        ));
        assert!(matches!(
            save_to_path(&link, &DesktopPreferences::default()),
            Err(PreferencesError::Symlink(_))
        ));

        fs::remove_file(&link).unwrap();
        save_to_path(&link, &DesktopPreferences::default()).unwrap();
        assert_eq!(
            fs::metadata(&link).unwrap().permissions().mode() & 0o777,
            0o600
        );
    }

    #[test]
    fn validation_failure_preserves_previous_file_and_creates_no_temp() {
        let directory = TestDirectory::new("preserve");
        let path = directory.path().join("desktop.json");
        save_to_path(&path, &valid_preferences()).unwrap();
        let before = fs::read(&path).unwrap();

        let mut invalid = DesktopPreferences::default();
        invalid
            .bindings
            .insert("unknown".into(), vec!["ctrl+x".into()]);
        assert!(save_to_path(&path, &invalid).is_err());
        assert_eq!(fs::read(&path).unwrap(), before);
        assert_eq!(
            fs::read_dir(directory.path())
                .unwrap()
                .filter_map(Result::ok)
                .filter(|entry| entry.file_name().to_string_lossy().contains(".tmp-"))
                .count(),
            0
        );
    }
}
