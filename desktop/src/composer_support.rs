//! Pure, bounded composer support shared by presentation code.
//!
//! This module deliberately has no GPUI or Snow RPC dependencies. Filesystem
//! discovery uses only `std::fs`; it never invokes a subprocess and never
//! follows a directory or file symlink.

use std::{
    collections::HashSet,
    error::Error,
    fmt,
    fs::{self, File, Metadata, OpenOptions},
    io::{self, Read},
    path::{Component, Path, PathBuf},
};

pub const LARGE_PASTE_RUNE_THRESHOLD: usize = 4 * 1024;
pub const LARGE_PASTE_LINE_THRESHOLD: usize = 40;

const IGNORED_MENTION_DIRS: &[&str] = &[".git", ".hg", ".svn", "node_modules", "vendor"];

pub const MENTION_CONTENT_LIMIT: usize = 256 * 1024;
pub const MENTION_TRUNCATION_MARKER: &str = "\n[content truncated by snow]\n";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct MentionLimits {
    /// Maximum directory entries inspected, including ignored and non-files.
    pub max_entries: usize,
    pub max_results: usize,
    /// Maximum aggregate UTF-8 bytes in returned project-relative paths.
    pub max_result_bytes: usize,
    pub max_depth: usize,
}

impl Default for MentionLimits {
    fn default() -> Self {
        Self {
            max_entries: 20_000,
            max_results: 2_000,
            max_result_bytes: 256 * 1024,
            max_depth: 64,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MentionDiscovery {
    root: PathBuf,
    pub files: Vec<String>,
    pub visited_entries: usize,
    pub result_bytes: usize,
    /// True when an entry, depth, result-count, or result-byte bound cut work.
    pub truncated: bool,
}

#[derive(Debug)]
pub enum MentionError {
    InvalidRoot,
    Io(io::Error),
    UnknownFile,
    UnsafePath,
    NotRegularFile,
    BinaryFile,
    ExpansionTooLarge,
}

impl fmt::Display for MentionError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidRoot => f.write_str("mention root must be a real, non-symlink directory"),
            Self::Io(error) => write!(f, "mention I/O: {error}"),
            Self::UnknownFile => f.write_str("file is not in the discovered mention catalog"),
            Self::UnsafePath => {
                f.write_str("mention path escapes the project root or uses a symlink")
            }
            Self::NotRegularFile => f.write_str("mention path is not a regular file"),
            Self::BinaryFile => f.write_str("mention file is not bounded UTF-8 text"),
            Self::ExpansionTooLarge => {
                f.write_str("expanded mention prompt exceeds its byte limit")
            }
        }
    }
}

impl Error for MentionError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        match self {
            Self::Io(error) => Some(error),
            _ => None,
        }
    }
}

impl From<io::Error> for MentionError {
    fn from(value: io::Error) -> Self {
        Self::Io(value)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MentionContent {
    pub text: String,
    pub truncated: bool,
}

impl MentionDiscovery {
    /// Discover regular files below `root`. The root itself must not be a
    /// symlink. Unreadable descendants are skipped, while an unreadable root is
    /// an error. Paths are normalized to `/` for insertion in composer text.
    pub fn discover(root: impl AsRef<Path>, limits: MentionLimits) -> Result<Self, MentionError> {
        let supplied_root = root.as_ref();
        let metadata = fs::symlink_metadata(supplied_root).map_err(MentionError::Io)?;
        if metadata.file_type().is_symlink() || !metadata.is_dir() {
            return Err(MentionError::InvalidRoot);
        }
        let root = fs::canonicalize(supplied_root).map_err(MentionError::Io)?;
        let mut discovery = Self {
            root: root.clone(),
            files: Vec::new(),
            visited_entries: 0,
            result_bytes: 0,
            truncated: false,
        };
        if limits.max_entries == 0
            || limits.max_results == 0
            || limits.max_result_bytes == 0
            || limits.max_depth == 0
        {
            discovery.truncated = true;
            return Ok(discovery);
        }

        enum WalkItem {
            Directory(PathBuf, usize),
            Entry(fs::DirEntry, usize),
        }

        // Entries are pushed in reverse lexical order so the LIFO stack visits
        // the complete relative path space in lexical depth-first order.
        let mut pending = vec![WalkItem::Directory(root.clone(), 0usize)];
        'walk: while let Some(item) = pending.pop() {
            match item {
                WalkItem::Directory(directory, depth) => {
                    if discovery.visited_entries >= limits.max_entries {
                        discovery.truncated = true;
                        continue;
                    }
                    let entry_iterator = match fs::read_dir(&directory) {
                        Ok(entries) => entries,
                        Err(error) if directory == root => return Err(MentionError::Io(error)),
                        Err(_) => continue,
                    };
                    let remaining = limits.max_entries - discovery.visited_entries;
                    let mut entries = Vec::new();
                    let mut inspected = 0usize;
                    for result in entry_iterator.take(remaining.saturating_add(1)) {
                        if inspected >= remaining {
                            discovery.truncated = true;
                            break;
                        }
                        inspected += 1;
                        discovery.visited_entries += 1;
                        if let Ok(entry) = result {
                            entries.push(entry);
                        }
                    }
                    entries.sort_by_key(|entry| std::cmp::Reverse(entry.file_name()));
                    pending.extend(
                        entries
                            .into_iter()
                            .map(|entry| WalkItem::Entry(entry, depth)),
                    );
                }
                WalkItem::Entry(entry, depth) => {
                    let file_type = match entry.file_type() {
                        Ok(file_type) => file_type,
                        Err(_) => continue,
                    };
                    if file_type.is_symlink() {
                        continue;
                    }
                    if file_type.is_dir() {
                        if ignored_mention_dir(&entry.file_name()) {
                            continue;
                        }
                        if depth + 1 >= limits.max_depth {
                            discovery.truncated = true;
                        } else {
                            pending.push(WalkItem::Directory(entry.path(), depth + 1));
                        }
                        continue;
                    }
                    if !file_type.is_file() {
                        continue;
                    }
                    if discovery.files.len() >= limits.max_results {
                        discovery.truncated = true;
                        break 'walk;
                    }
                    let entry_path = entry.path();
                    let relative = match entry_path.strip_prefix(&root) {
                        Ok(relative) => relative,
                        Err(_) => continue,
                    };
                    let Some(relative) = portable_relative_path(relative) else {
                        continue;
                    };
                    // Composer mention tokens cannot represent whitespace in a
                    // path without quoting, which neither surface supports.
                    if relative.chars().any(char::is_whitespace) {
                        continue;
                    }
                    let path_bytes = relative.len();
                    if path_bytes
                        > limits
                            .max_result_bytes
                            .saturating_sub(discovery.result_bytes)
                    {
                        discovery.truncated = true;
                        continue;
                    }
                    discovery.result_bytes += path_bytes;
                    discovery.files.push(relative);
                }
            }
        }
        discovery.files.sort();
        Ok(discovery)
    }

    pub fn root(&self) -> &Path {
        &self.root
    }

    /// Read a catalogued mention as UTF-8 text without reading more than
    /// `max_bytes + 4` bytes. A longer file is cut at a UTF-8 boundary.
    pub fn read_text(
        &self,
        relative: &str,
        max_bytes: usize,
    ) -> Result<MentionContent, MentionError> {
        if !self.files.iter().any(|known| known == relative) {
            return Err(MentionError::UnknownFile);
        }
        let relative_path = validated_relative_path(relative)?;
        let path = self.root.join(&relative_path);
        reject_symlink_components(&self.root, &relative_path)?;
        let canonical = fs::canonicalize(&path).map_err(MentionError::Io)?;
        if !canonical.starts_with(&self.root) {
            return Err(MentionError::UnsafePath);
        }
        let expected = fs::symlink_metadata(&path).map_err(MentionError::Io)?;
        if expected.file_type().is_symlink() {
            return Err(MentionError::UnsafePath);
        }
        if !expected.is_file() {
            return Err(MentionError::NotRegularFile);
        }

        let mut file = open_regular_no_follow(&path, &expected)?;
        // Recheck parent components after opening. On Unix, O_NOFOLLOW and
        // device/inode identity additionally bind the opened final component.
        // Portable std has no openat-style component walk; non-Unix builds
        // therefore fail closed for observed symlinks but cannot eliminate a
        // malicious concurrent parent-directory replacement race.
        reject_symlink_components(&self.root, &relative_path)?;
        let canonical = fs::canonicalize(&path).map_err(MentionError::Io)?;
        if !canonical.starts_with(&self.root) {
            return Err(MentionError::UnsafePath);
        }

        let read_limit = max_bytes.saturating_add(4).saturating_add(1);
        let mut bytes = Vec::with_capacity(read_limit.min(64 * 1024));
        file.by_ref()
            .take(u64::try_from(read_limit).unwrap_or(u64::MAX))
            .read_to_end(&mut bytes)?;
        if bytes.contains(&0) {
            return Err(MentionError::BinaryFile);
        }
        let opened_length = file.metadata().map_err(MentionError::Io)?.len();
        let truncated =
            opened_length > u64::try_from(max_bytes).unwrap_or(u64::MAX) || bytes.len() > max_bytes;
        // Validate the bounded lookahead before cutting it. This distinguishes
        // a valid multibyte rune crossing the content boundary from malformed
        // bytes that merely look like an incomplete boundary rune after cut.
        if let Err(error) = std::str::from_utf8(&bytes)
            && (!truncated || error.valid_up_to() < max_bytes)
        {
            return Err(MentionError::BinaryFile);
        }
        if truncated {
            bytes.truncate(max_bytes);
        }
        let valid_length = match std::str::from_utf8(&bytes) {
            Ok(_) => bytes.len(),
            Err(error) if truncated && error.error_len().is_none() => error.valid_up_to(),
            Err(_) => return Err(MentionError::BinaryFile),
        };
        bytes.truncate(valid_length);
        let text = String::from_utf8(bytes).map_err(|_| MentionError::BinaryFile)?;
        Ok(MentionContent { text, truncated })
    }
}

fn ignored_mention_dir(name: &std::ffi::OsStr) -> bool {
    name.to_str()
        .is_some_and(|name| IGNORED_MENTION_DIRS.iter().any(|ignored| name == *ignored))
}

fn portable_relative_path(path: &Path) -> Option<String> {
    let mut output = String::new();
    for component in path.components() {
        let Component::Normal(part) = component else {
            return None;
        };
        let part = part.to_str()?;
        if !output.is_empty() {
            output.push('/');
        }
        output.push_str(part);
    }
    (!output.is_empty()).then_some(output)
}

fn validated_relative_path(relative: &str) -> Result<PathBuf, MentionError> {
    if relative.is_empty() || relative.contains('\0') || relative.contains('\\') {
        return Err(MentionError::UnsafePath);
    }
    let path = Path::new(relative);
    if path.is_absolute()
        || path
            .components()
            .any(|component| !matches!(component, Component::Normal(_)))
    {
        return Err(MentionError::UnsafePath);
    }
    Ok(path.to_path_buf())
}

fn reject_symlink_components(root: &Path, relative: &Path) -> Result<(), MentionError> {
    let mut current = root.to_path_buf();
    for component in relative.components() {
        let Component::Normal(part) = component else {
            return Err(MentionError::UnsafePath);
        };
        current.push(part);
        let metadata = fs::symlink_metadata(&current).map_err(MentionError::Io)?;
        if metadata.file_type().is_symlink() {
            return Err(MentionError::UnsafePath);
        }
    }
    Ok(())
}

#[cfg(any(
    target_os = "linux",
    target_os = "android",
    target_os = "macos",
    target_os = "ios",
    target_os = "freebsd",
    target_os = "openbsd",
    target_os = "netbsd",
    target_os = "dragonfly"
))]
fn open_regular_no_follow(path: &Path, expected: &Metadata) -> Result<File, MentionError> {
    use std::os::unix::fs::{MetadataExt, OpenOptionsExt};

    // O_NOFOLLOW values from the supported Unix platform headers. Keeping the
    // constants local avoids adding libc solely for this one flag.
    #[cfg(any(target_os = "linux", target_os = "android"))]
    const O_NOFOLLOW: i32 = 0o00400000;
    #[cfg(any(
        target_os = "macos",
        target_os = "ios",
        target_os = "freebsd",
        target_os = "openbsd",
        target_os = "netbsd",
        target_os = "dragonfly"
    ))]
    const O_NOFOLLOW: i32 = 0x00000100;

    let file = OpenOptions::new()
        .read(true)
        .custom_flags(O_NOFOLLOW)
        .open(path)
        .map_err(MentionError::Io)?;
    let opened = file.metadata().map_err(MentionError::Io)?;
    if !opened.is_file() {
        return Err(MentionError::NotRegularFile);
    }
    if expected.dev() != opened.dev() || expected.ino() != opened.ino() {
        return Err(MentionError::UnsafePath);
    }
    Ok(file)
}

#[cfg(not(any(
    target_os = "linux",
    target_os = "android",
    target_os = "macos",
    target_os = "ios",
    target_os = "freebsd",
    target_os = "openbsd",
    target_os = "netbsd",
    target_os = "dragonfly"
)))]
fn open_regular_no_follow(path: &Path, _expected: &Metadata) -> Result<File, MentionError> {
    // Rust std exposes no portable O_NOFOLLOW/openat equivalent. This is used
    // on non-Unix and Unix targets without a locally verified flag value.
    // Callers check
    // every component immediately before and after this open and fail closed
    // whenever either check observes a symlink.
    let file = OpenOptions::new()
        .read(true)
        .open(path)
        .map_err(MentionError::Io)?;
    if !file.metadata().map_err(MentionError::Io)?.is_file() {
        return Err(MentionError::NotRegularFile);
    }
    Ok(file)
}

/// Return the trailing `@token` and its UTF-8 byte offset. Completion only
/// considers the token at the end of the composer, matching the TUI behavior.
pub fn mention_query(text: &str) -> Option<(&str, usize)> {
    let start = text
        .bytes()
        .rposition(|byte| matches!(byte, b' ' | b'\t' | b'\n'))
        .map_or(0, |index| index + 1);
    let token = text.get(start..)?;
    if token.bytes().any(|byte| matches!(byte, b'\r' | b'\n')) {
        return None;
    }
    token.strip_prefix('@').map(|query| (query, start))
}

/// Path-prefix matches precede basename-prefix matches. Both groups are sorted.
pub fn match_mentions(files: &[String], query: &str, limit: usize) -> Vec<String> {
    if limit == 0 {
        return Vec::new();
    }
    let query = query.replace('\\', "/").to_lowercase();
    // Keep only the lexically first bounded candidates while scanning. This
    // avoids temporarily cloning every match when handed a non-discovery list.
    let mut paths = Vec::new();
    let mut basenames = Vec::new();
    for path in files {
        let lower = path.to_lowercase();
        if query.is_empty() || lower.starts_with(&query) {
            insert_lexically_bounded(&mut paths, path, limit);
        } else if lower
            .rsplit('/')
            .next()
            .is_some_and(|basename| basename.starts_with(&query))
        {
            insert_lexically_bounded(&mut basenames, path, limit);
        }
    }
    paths
        .into_iter()
        .chain(basenames)
        .take(limit)
        .cloned()
        .collect()
}

fn insert_lexically_bounded<'a>(values: &mut Vec<&'a String>, value: &'a String, limit: usize) {
    let index = values
        .binary_search_by(|candidate| candidate.as_str().cmp(value.as_str()))
        .unwrap_or_else(|index| index);
    values.insert(index, value);
    if values.len() > limit {
        values.pop();
    }
}

pub fn replace_mention_token(text: &str, start: usize, path: &str) -> Option<String> {
    if !text.get(start..)?.starts_with('@')
        || path.is_empty()
        || path.chars().any(char::is_whitespace)
    {
        return None;
    }
    Some(format!("{}@{path} ", text.get(..start)?))
}

/// Expand known ASCII-delimited `@path` tokens into XML-safe bounded file
/// blocks. Unknown, unreadable, empty, unsafe, and binary mentions remain
/// literal. `max_output_bytes` bounds the complete returned prompt.
pub fn expand_mention_prompt(
    text: &str,
    discovery: &MentionDiscovery,
    max_output_bytes: usize,
) -> Result<String, MentionError> {
    let known = discovery
        .files
        .iter()
        .map(String::as_str)
        .collect::<HashSet<_>>();
    let mut output = String::with_capacity(text.len().min(max_output_bytes));
    let bytes = text.as_bytes();
    let mut cursor = 0usize;
    let mut index = 0usize;
    while index < bytes.len() {
        let boundary = index == 0 || is_ascii_mention_space(bytes[index - 1]);
        if bytes[index] != b'@' || !boundary {
            index += 1;
            continue;
        }
        let mut end = index + 1;
        while end < bytes.len() && !is_ascii_mention_space(bytes[end]) {
            end += 1;
        }
        let Some(path) = text.get(index + 1..end) else {
            index += 1;
            continue;
        };
        if !known.contains(path) {
            index = end;
            continue;
        }
        let content = match discovery.read_text(path, MENTION_CONTENT_LIMIT) {
            Ok(content) if !content.text.is_empty() => content,
            _ => {
                index = end;
                continue;
            }
        };

        push_mention_output(&mut output, &text[cursor..index], max_output_bytes)?;
        push_mention_output(&mut output, "<file name=\"", max_output_bytes)?;
        push_xml_escaped(&mut output, path, max_output_bytes)?;
        push_mention_output(&mut output, "\">\n", max_output_bytes)?;
        push_xml_escaped(&mut output, &content.text, max_output_bytes)?;
        if content.truncated {
            push_mention_output(&mut output, MENTION_TRUNCATION_MARKER, max_output_bytes)?;
        } else if !content.text.ends_with('\n') {
            push_mention_output(&mut output, "\n", max_output_bytes)?;
        }
        push_mention_output(&mut output, "</file>", max_output_bytes)?;
        cursor = end;
        index = end;
    }
    push_mention_output(&mut output, &text[cursor..], max_output_bytes)?;
    Ok(output)
}

fn is_ascii_mention_space(byte: u8) -> bool {
    matches!(byte, b' ' | b'\t' | b'\n' | b'\r')
}

fn push_xml_escaped(
    output: &mut String,
    value: &str,
    max_output_bytes: usize,
) -> Result<(), MentionError> {
    let mut cursor = 0usize;
    for (index, character) in value.char_indices() {
        let escaped = match character {
            '&' => "&amp;",
            '<' => "&lt;",
            '>' => "&gt;",
            '\"' => "&quot;",
            '\'' => "&apos;",
            _ if !is_xml_character(character) => "\u{fffd}",
            _ => continue,
        };
        push_mention_output(output, &value[cursor..index], max_output_bytes)?;
        push_mention_output(output, escaped, max_output_bytes)?;
        cursor = index + character.len_utf8();
    }
    push_mention_output(output, &value[cursor..], max_output_bytes)
}

fn is_xml_character(character: char) -> bool {
    matches!(character, '\t' | '\n' | '\r')
        || matches!(character as u32, 0x20..=0xd7ff | 0xe000..=0xfffd | 0x10000..=0x10ffff)
}

fn push_mention_output(
    output: &mut String,
    value: &str,
    max_output_bytes: usize,
) -> Result<(), MentionError> {
    if value.len() > max_output_bytes.saturating_sub(output.len()) {
        return Err(MentionError::ExpansionTooLarge);
    }
    output.push_str(value);
    Ok(())
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SkillSpec {
    pub name: String,
    pub description: String,
    pub enabled: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct CompletionLimits {
    pub max_catalog_items: usize,
    pub max_results: usize,
    pub max_result_bytes: usize,
}

impl Default for CompletionLimits {
    fn default() -> Self {
        Self {
            max_catalog_items: 2_000,
            max_results: 100,
            max_result_bytes: 64 * 1024,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SkillCompletion {
    pub query: String,
    pub token_start: usize,
    pub matches: Vec<SkillSpec>,
    pub truncated: bool,
}

/// Complete an enabled `$skill` at the end of `text`. Earlier exact `$name`
/// whitespace tokens are omitted, and duplicate catalog names collapse to one.
pub fn complete_skills(
    text: &str,
    catalog: &[SkillSpec],
    limits: CompletionLimits,
) -> Option<SkillCompletion> {
    let token_start = trailing_token_start(text);
    let query = text.get(token_start..)?.strip_prefix('$')?.to_owned();
    let selected = text[..token_start]
        .split_whitespace()
        .filter_map(|token| token.strip_prefix('$'))
        .filter(|name| !name.is_empty())
        .collect::<HashSet<_>>();
    let query_folded = query.to_lowercase();
    let mut truncated = catalog.len() > limits.max_catalog_items;
    let mut candidates = catalog
        .iter()
        .take(limits.max_catalog_items)
        .filter(|skill| {
            skill.enabled
                && valid_token_name(&skill.name)
                && !selected.contains(skill.name.as_str())
                && (query.is_empty() || skill.name.to_lowercase().starts_with(&query_folded))
        })
        .collect::<Vec<_>>();
    candidates.sort_by(|left, right| left.name.cmp(&right.name));
    candidates.dedup_by(|left, right| left.name == right.name);

    let mut matches = Vec::new();
    let mut result_bytes = 0usize;
    for skill in candidates {
        let bytes = skill.name.len().saturating_add(skill.description.len());
        if matches.len() >= limits.max_results
            || bytes > limits.max_result_bytes.saturating_sub(result_bytes)
        {
            truncated = true;
            continue;
        }
        result_bytes += bytes;
        matches.push(skill.clone());
    }
    Some(SkillCompletion {
        query,
        token_start,
        matches,
        truncated,
    })
}

pub fn replace_skill_token(text: &str, token_start: usize, name: &str) -> Option<String> {
    if !text.get(token_start..)?.starts_with('$') || !valid_token_name(name) {
        return None;
    }
    Some(format!("{}${name} ", text.get(..token_start)?))
}

fn valid_token_name(name: &str) -> bool {
    !name.is_empty() && !name.starts_with('$') && !name.chars().any(char::is_whitespace)
}

fn trailing_token_start(text: &str) -> usize {
    text.char_indices()
        .filter_map(|(index, character)| {
            character
                .is_whitespace()
                .then_some(index + character.len_utf8())
        })
        .last()
        .unwrap_or(0)
}

/// Mirrors the desktop command catalog's completion behavior without taking a
/// dependency on its serde/RPC-heavy module.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CommandCompletion {
    /// The command rejects arguments and Enter may execute it immediately.
    Immediate,
    /// The command has optional or required arguments and remains editable.
    Editable,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SlashCommand {
    pub name: String,
    pub description: String,
    pub completion: CommandCompletion,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SlashKey {
    Up,
    Down,
    Tab,
    BackTab,
    Enter,
    Escape,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SlashAction {
    Ignored,
    SelectionChanged,
    Dismissed,
    Insert(String),
    Execute(String),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SlashSelectionState {
    pub visible: bool,
    pub selected: usize,
    pub matches: Vec<SlashCommand>,
    pub truncated: bool,
    limits: CompletionLimits,
}

impl SlashSelectionState {
    pub fn new(limits: CompletionLimits) -> Self {
        Self {
            visible: false,
            selected: 0,
            matches: Vec::new(),
            truncated: false,
            limits,
        }
    }

    /// Recompute matches from a slash-command first token. Exact and prefix
    /// matches retain catalog order and precede stable subsequence matches.
    pub fn refresh(&mut self, editor_text: &str, catalog: &[SlashCommand]) {
        self.matches.clear();
        self.truncated = false;
        if !editor_text.starts_with('/') || editor_text.chars().any(char::is_whitespace) {
            self.visible = false;
            self.selected = 0;
            return;
        }
        self.visible = true;
        let query = editor_text[1..].to_lowercase();
        let mut prefix = Vec::new();
        let mut fuzzy = Vec::new();
        for command in catalog.iter().take(self.limits.max_catalog_items) {
            let Some(name) = normalized_command_name(&command.name) else {
                continue;
            };
            let folded = name.to_lowercase();
            if query.is_empty() || folded.starts_with(&query) {
                prefix.push(command);
            } else if query.chars().count() >= 3 && subsequence_match(&folded, &query) {
                fuzzy.push(command);
            }
        }
        self.truncated = catalog.len() > self.limits.max_catalog_items;
        prefix.extend(fuzzy);
        let mut bytes = 0usize;
        for command in prefix {
            if self
                .matches
                .iter()
                .any(|accepted| accepted.name == command.name)
            {
                continue;
            }
            let command_bytes = command.name.len().saturating_add(command.description.len());
            if self.matches.len() >= self.limits.max_results
                || command_bytes > self.limits.max_result_bytes.saturating_sub(bytes)
            {
                self.truncated = true;
                continue;
            }
            bytes += command_bytes;
            self.matches.push(command.clone());
        }
        if self.selected >= self.matches.len() {
            self.selected = 0;
        }
    }

    pub fn selected_command(&self) -> Option<&SlashCommand> {
        self.visible
            .then(|| self.matches.get(self.selected))
            .flatten()
    }

    pub fn handle_key(&mut self, key: SlashKey) -> SlashAction {
        if !self.visible {
            return SlashAction::Ignored;
        }
        match key {
            SlashKey::Escape => {
                self.visible = false;
                SlashAction::Dismissed
            }
            SlashKey::Up | SlashKey::BackTab => {
                if self.matches.is_empty() {
                    return SlashAction::Ignored;
                }
                self.selected = (self.selected + self.matches.len() - 1) % self.matches.len();
                SlashAction::SelectionChanged
            }
            SlashKey::Down => {
                if self.matches.is_empty() {
                    return SlashAction::Ignored;
                }
                self.selected = (self.selected + 1) % self.matches.len();
                SlashAction::SelectionChanged
            }
            SlashKey::Tab | SlashKey::Enter => {
                let Some(command) = self.selected_command().cloned() else {
                    return SlashAction::Ignored;
                };
                self.visible = false;
                let insertion = match command.completion {
                    CommandCompletion::Immediate => command.name.clone(),
                    CommandCompletion::Editable => format!("{} ", command.name),
                };
                if key == SlashKey::Tab || command.completion == CommandCompletion::Editable {
                    SlashAction::Insert(insertion)
                } else {
                    SlashAction::Execute(command.name)
                }
            }
        }
    }
}

fn normalized_command_name(name: &str) -> Option<&str> {
    let name = name.strip_prefix('/')?;
    (!name.is_empty() && !name.chars().any(char::is_whitespace)).then_some(name)
}

fn subsequence_match(value: &str, query: &str) -> bool {
    let mut query = query.chars();
    let mut wanted = query.next();
    for character in value.chars() {
        if wanted == Some(character) {
            wanted = query.next();
        }
    }
    wanted.is_none()
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PasteLimits {
    pub max_attachments: usize,
    pub max_single_bytes: usize,
    pub max_total_bytes: usize,
    pub max_expanded_bytes: usize,
}

impl Default for PasteLimits {
    fn default() -> Self {
        Self {
            max_attachments: 16,
            // A JSON string can expand an input byte to a six-byte `\u00XX`
            // escape. Keeping expanded text at 2 MiB leaves conservative room
            // below Snow RPC's 16 MiB JSONL frame cap for escaping and fields.
            max_single_bytes: 2 * 1024 * 1024,
            max_total_bytes: 2 * 1024 * 1024,
            max_expanded_bytes: 2 * 1024 * 1024,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PasteError {
    TooManyAttachments,
    PasteTooLarge,
    TotalTooLarge,
    ExpansionTooLarge,
    IdExhausted,
    InvalidRecovery,
}

impl fmt::Display for PasteError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::TooManyAttachments => f.write_str("too many collapsed pastes"),
            Self::PasteTooLarge => f.write_str("paste exceeds the single-paste byte limit"),
            Self::TotalTooLarge => f.write_str("collapsed pastes exceed the aggregate byte limit"),
            Self::ExpansionTooLarge => f.write_str("expanded composer exceeds its byte limit"),
            Self::IdExhausted => f.write_str("collapsed paste identifier space is exhausted"),
            Self::InvalidRecovery => f.write_str("collapsed paste recovery is invalid or stale"),
        }
    }
}

impl Error for PasteError {}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CollapsedPaste {
    pub id: u64,
    pub token: String,
    text: String,
}

impl CollapsedPaste {
    pub fn text(&self) -> &str {
        &self.text
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PasteStore {
    limits: PasteLimits,
    next_id: u64,
    total_bytes: usize,
    attachments: Vec<CollapsedPaste>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PasteSubmission {
    compact: String,
    expanded: String,
    attachments: Vec<CollapsedPaste>,
}

impl PasteSubmission {
    pub fn compact_text(&self) -> &str {
        &self.compact
    }

    pub fn expanded_text(&self) -> &str {
        &self.expanded
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DeleteDirection {
    Backward,
    Forward,
}

impl PasteStore {
    pub fn new(limits: PasteLimits) -> Self {
        Self {
            limits,
            next_id: 0,
            total_bytes: 0,
            attachments: Vec::new(),
        }
    }

    pub fn attachments(&self) -> &[CollapsedPaste] {
        &self.attachments
    }

    pub fn total_bytes(&self) -> usize {
        self.total_bytes
    }

    pub fn should_collapse(text: &str) -> bool {
        text.chars().count() >= LARGE_PASTE_RUNE_THRESHOLD
            || text.chars().filter(|character| *character == '\n').count() + 1
                >= LARGE_PASTE_LINE_THRESHOLD
    }

    /// Store a large paste and return its compact token. Small pastes return
    /// `Ok(None)` and should remain directly editable.
    pub fn collapse(&mut self, text: String) -> Result<Option<String>, PasteError> {
        if !Self::should_collapse(&text) {
            return Ok(None);
        }
        if self.attachments.len() >= self.limits.max_attachments {
            return Err(PasteError::TooManyAttachments);
        }
        if text.len() > self.limits.max_single_bytes {
            return Err(PasteError::PasteTooLarge);
        }
        if text.len() > self.limits.max_total_bytes.saturating_sub(self.total_bytes) {
            return Err(PasteError::TotalTooLarge);
        }
        let id = self.next_id.checked_add(1).ok_or(PasteError::IdExhausted)?;
        self.next_id = id;
        let runes = text.chars().count();
        let lines = text.chars().filter(|character| *character == '\n').count() + 1;
        let detail = if lines > 1 {
            format!("{lines} lines · {runes} chars")
        } else {
            format!("{runes} chars")
        };
        let token = format!("[Pasted text #{id} · {detail}]");
        self.total_bytes += text.len();
        self.attachments.push(CollapsedPaste {
            id,
            token: token.clone(),
            text,
        });
        Ok(Some(token))
    }

    /// Expand each stored token at most once. Inserted paste bodies are never
    /// rescanned, so token-looking text inside a paste remains literal.
    pub fn expand(&self, compact: &str) -> Result<String, PasteError> {
        expand_once(compact, &self.attachments, self.limits.max_expanded_bytes)
    }

    /// Remove one attachment and its first token occurrence from the draft.
    pub fn remove(&mut self, compact: &mut String, id: u64) -> bool {
        let Some(index) = self
            .attachments
            .iter()
            .position(|attachment| attachment.id == id)
        else {
            return false;
        };
        let attachment = self.attachments.remove(index);
        if let Some(token_start) = compact.find(&attachment.token) {
            compact.replace_range(token_start..token_start + attachment.token.len(), "");
        }
        self.total_bytes = self.total_bytes.saturating_sub(attachment.text.len());
        true
    }

    pub fn remove_last(&mut self, compact: &mut String) -> bool {
        self.attachments
            .last()
            .map(|attachment| attachment.id)
            .is_some_and(|id| self.remove(compact, id))
    }

    /// Treat a collapsed token as one editor atom when backspace/delete lands
    /// within or immediately adjacent to it. `cursor_runes` is a rune offset.
    pub fn remove_at_cursor(
        &mut self,
        compact: &mut String,
        cursor_runes: usize,
        direction: DeleteDirection,
    ) -> bool {
        let value = compact.chars().collect::<Vec<_>>();
        let mut remove_id = None;
        'attachments: for attachment in &self.attachments {
            let token = attachment.token.chars().collect::<Vec<_>>();
            if token.len() > value.len() {
                continue;
            }
            for start in 0..=value.len() - token.len() {
                if value[start..start + token.len()] != token {
                    continue;
                }
                let end = start + token.len();
                let touches = match direction {
                    DeleteDirection::Backward => cursor_runes > start && cursor_runes <= end,
                    DeleteDirection::Forward => cursor_runes >= start && cursor_runes < end,
                };
                if touches {
                    remove_id = Some(attachment.id);
                    break 'attachments;
                }
            }
        }
        remove_id.is_some_and(|id| self.remove(compact, id))
    }

    /// Drop bodies whose tokens were edited out by ordinary composer edits.
    pub fn prune(&mut self, compact: &str) {
        self.attachments.retain(|attachment| {
            if compact.contains(&attachment.token) {
                true
            } else {
                self.total_bytes = self.total_bytes.saturating_sub(attachment.text.len());
                false
            }
        });
    }

    /// Expand and drain referenced bodies for a submission. Preserve the
    /// returned value until admission is known; `recover` restores a rejection.
    pub fn prepare_submission(&mut self, compact: String) -> Result<PasteSubmission, PasteError> {
        self.prune(&compact);
        let expanded = self.expand(&compact)?;
        let attachments = std::mem::take(&mut self.attachments);
        self.total_bytes = 0;
        Ok(PasteSubmission {
            compact,
            expanded,
            attachments,
        })
    }

    pub fn recover(&mut self, submission: PasteSubmission) -> Result<String, PasteError> {
        let restored_bytes = submission
            .attachments
            .iter()
            .try_fold(0usize, |total, attachment| {
                total.checked_add(attachment.text.len())
            })
            .ok_or(PasteError::InvalidRecovery)?;
        let ids_unique = submission
            .attachments
            .iter()
            .map(|attachment| attachment.id)
            .collect::<HashSet<_>>()
            .len()
            == submission.attachments.len();
        let tokens_referenced = submission
            .attachments
            .iter()
            .all(|attachment| submission.compact.contains(&attachment.token));
        if !self.attachments.is_empty()
            || !ids_unique
            || !tokens_referenced
            || submission.attachments.len() > self.limits.max_attachments
            || submission
                .attachments
                .iter()
                .any(|attachment| attachment.text.len() > self.limits.max_single_bytes)
            || restored_bytes > self.limits.max_total_bytes
        {
            return Err(PasteError::InvalidRecovery);
        }
        self.next_id = self.next_id.max(
            submission
                .attachments
                .iter()
                .map(|attachment| attachment.id)
                .max()
                .unwrap_or(0),
        );
        self.total_bytes = restored_bytes;
        self.attachments = submission.attachments;
        Ok(submission.compact)
    }
}

impl Default for PasteStore {
    fn default() -> Self {
        Self::new(PasteLimits::default())
    }
}

fn expand_once(
    compact: &str,
    attachments: &[CollapsedPaste],
    max_output_bytes: usize,
) -> Result<String, PasteError> {
    if compact.len() > max_output_bytes {
        return Err(PasteError::ExpansionTooLarge);
    }
    let mut output = String::with_capacity(compact.len());
    let mut remaining = compact;
    let mut used = vec![false; attachments.len()];
    while !remaining.is_empty() {
        let mut best: Option<(usize, usize)> = None;
        for (attachment_index, attachment) in attachments.iter().enumerate() {
            if used[attachment_index] {
                continue;
            }
            if let Some(index) = remaining.find(&attachment.token) {
                if best.is_none_or(|(best_index, _)| index < best_index) {
                    best = Some((index, attachment_index));
                }
            }
        }
        let Some((index, attachment_index)) = best else {
            checked_push(&mut output, remaining, max_output_bytes)?;
            break;
        };
        checked_push(&mut output, &remaining[..index], max_output_bytes)?;
        checked_push(
            &mut output,
            &attachments[attachment_index].text,
            max_output_bytes,
        )?;
        remaining = &remaining[index + attachments[attachment_index].token.len()..];
        used[attachment_index] = true;
    }
    Ok(output)
}

fn checked_push(output: &mut String, value: &str, max_bytes: usize) -> Result<(), PasteError> {
    if value.len() > max_bytes.saturating_sub(output.len()) {
        return Err(PasteError::ExpansionTooLarge);
    }
    output.push_str(value);
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{
        sync::atomic::{AtomicU64, Ordering},
        time::{SystemTime, UNIX_EPOCH},
    };

    static NEXT_TEMP: AtomicU64 = AtomicU64::new(0);

    struct TestDir(PathBuf);

    impl TestDir {
        fn new() -> Self {
            let unique = NEXT_TEMP.fetch_add(1, Ordering::Relaxed);
            let nanos = SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos();
            let path = std::env::temp_dir().join(format!(
                "snow-composer-support-{}-{nanos}-{unique}",
                std::process::id()
            ));
            fs::create_dir(&path).unwrap();
            Self(path)
        }

        fn path(&self) -> &Path {
            &self.0
        }

        fn write(&self, relative: &str, contents: &[u8]) {
            let path = self.0.join(relative);
            fs::create_dir_all(path.parent().unwrap()).unwrap();
            fs::write(path, contents).unwrap();
        }
    }

    impl Drop for TestDir {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    #[test]
    fn mention_discovery_is_rooted_regular_sorted_and_ignored() {
        let root = TestDir::new();
        root.write("z.md", b"z");
        root.write("src/main.rs", b"main");
        for path in [
            ".git/config",
            ".hg/store",
            ".svn/entries",
            "node_modules/package/index.js",
            "vendor/dependency.go",
        ] {
            root.write(path, b"ignored");
        }
        root.write("target/debug/output", b"not ignored by TUI");
        root.write(".idea/keep.xml", b"not ignored by TUI");
        root.write("two words.txt", b"unsupported token path");
        fs::create_dir(root.path().join("empty")).unwrap();

        let found = MentionDiscovery::discover(root.path(), MentionLimits::default()).unwrap();
        let expected = [
            ".idea/keep.xml",
            "src/main.rs",
            "target/debug/output",
            "z.md",
        ];
        assert_eq!(found.files, expected);
        assert_eq!(
            found.result_bytes,
            expected.iter().map(|path| path.len()).sum::<usize>()
        );
        assert!(!found.truncated);
        assert_eq!(found.root(), fs::canonicalize(root.path()).unwrap());
        assert!(!found.files.iter().any(|path| path.contains("two words")));
        assert!(!ignored_mention_dir(std::ffi::OsStr::new(".GIT")));
    }

    #[cfg(unix)]
    #[test]
    fn mention_discovery_and_reads_never_follow_symlinks() {
        use std::os::unix::fs::symlink;

        let root = TestDir::new();
        let outside = TestDir::new();
        outside.write("outside.txt", b"outside");
        root.write("inside.txt", b"inside");
        symlink(
            outside.path().join("outside.txt"),
            root.path().join("linked.txt"),
        )
        .unwrap();
        symlink(outside.path(), root.path().join("linked-dir")).unwrap();

        let found = MentionDiscovery::discover(root.path(), MentionLimits::default()).unwrap();
        assert_eq!(found.files, ["inside.txt"]);
        assert_eq!(
            found.read_text("linked.txt", 100).unwrap_err().to_string(),
            "file is not in the discovered mention catalog"
        );
        // Replacing a formerly catalogued regular file with a symlink must
        // fail rather than following the replacement.
        fs::remove_file(root.path().join("inside.txt")).unwrap();
        symlink(
            outside.path().join("outside.txt"),
            root.path().join("inside.txt"),
        )
        .unwrap();
        assert!(found.read_text("inside.txt", 100).is_err());

        let linked_root = root.path().with_extension("link");
        symlink(root.path(), &linked_root).unwrap();
        assert!(matches!(
            MentionDiscovery::discover(&linked_root, MentionLimits::default()),
            Err(MentionError::InvalidRoot)
        ));
        fs::remove_file(linked_root).unwrap();
    }

    #[test]
    fn mention_discovery_enforces_count_byte_entry_and_depth_bounds() {
        let root = TestDir::new();
        for name in ["a", "bb", "ccc", "dddd"] {
            root.write(name, b"x");
        }
        root.write("deep/one/two/file", b"x");

        let by_count = MentionDiscovery::discover(
            root.path(),
            MentionLimits {
                max_results: 2,
                ..MentionLimits::default()
            },
        )
        .unwrap();
        assert_eq!(by_count.files, ["a", "bb"]);
        assert!(by_count.truncated);

        let by_bytes = MentionDiscovery::discover(
            root.path(),
            MentionLimits {
                max_result_bytes: 3,
                ..MentionLimits::default()
            },
        )
        .unwrap();
        assert!(by_bytes.result_bytes <= 3);
        assert!(by_bytes.truncated);

        let by_entries = MentionDiscovery::discover(
            root.path(),
            MentionLimits {
                max_entries: 1,
                ..MentionLimits::default()
            },
        )
        .unwrap();
        assert_eq!(by_entries.visited_entries, 1);
        assert!(by_entries.truncated);

        let by_depth = MentionDiscovery::discover(
            root.path(),
            MentionLimits {
                max_depth: 1,
                ..MentionLimits::default()
            },
        )
        .unwrap();
        assert!(!by_depth.files.iter().any(|path| path.contains("deep/")));
        assert!(by_depth.truncated);
    }

    #[test]
    fn bounded_discovery_keeps_lexically_first_nested_results() {
        let root = TestDir::new();
        for path in ["b.txt", "a/z.txt", "a/a.txt", "c/a.txt"] {
            root.write(path, b"x");
        }
        let found = MentionDiscovery::discover(
            root.path(),
            MentionLimits {
                max_results: 2,
                ..MentionLimits::default()
            },
        )
        .unwrap();
        assert_eq!(found.files, ["a/a.txt", "a/z.txt"]);
        assert!(found.truncated);
    }

    #[test]
    fn mention_prompt_expansion_is_xml_safe_and_leaves_bad_mentions_literal() {
        let root = TestDir::new();
        let special = "a\"<&'.txt";
        root.write(special, b"one <tag> & \" '\x01");
        root.write("empty.txt", b"");
        root.write("binary.dat", b"a\0b");
        root.write("gone.txt", b"gone");
        let found = MentionDiscovery::discover(root.path(), MentionLimits::default()).unwrap();
        fs::remove_file(root.path().join("gone.txt")).unwrap();

        let prompt = format!("see @{special} @unknown.txt @empty.txt @binary.dat @gone.txt");
        let expanded = expand_mention_prompt(&prompt, &found, 1024).unwrap();
        assert!(expanded.contains("<file name=\"a&quot;&lt;&amp;&apos;.txt\">"));
        assert!(expanded.contains("one &lt;tag&gt; &amp; &quot; &apos;\u{fffd}"));
        for literal in ["@unknown.txt", "@empty.txt", "@binary.dat", "@gone.txt"] {
            assert!(
                expanded.contains(literal),
                "missing literal {literal}: {expanded}"
            );
        }
        assert!(!expanded.contains("one <tag>"));
    }

    #[test]
    fn mention_prompt_uses_exact_truncation_marker_and_output_bound() {
        let root = TestDir::new();
        root.write("large.txt", "é".repeat(MENTION_CONTENT_LIMIT).as_bytes());
        let found = MentionDiscovery::discover(root.path(), MentionLimits::default()).unwrap();
        let expanded = expand_mention_prompt("@large.txt", &found, 2 * 1024 * 1024).unwrap();
        assert_eq!(expanded.matches(MENTION_TRUNCATION_MARKER).count(), 1);
        assert!(expanded.ends_with("\n[content truncated by snow]\n</file>"));
        assert!(matches!(
            expand_mention_prompt("@large.txt", &found, 100),
            Err(MentionError::ExpansionTooLarge)
        ));
    }

    #[test]
    fn mention_text_reads_are_catalogued_bounded_utf8_and_non_binary() {
        let root = TestDir::new();
        root.write("unicode.txt", "aébc".as_bytes());
        root.write("binary", b"a\0b");
        root.write("invalid-utf8", &[0xff, 0xfe]);
        root.write("invalid-at-boundary", b"aaa\xc2Arest");
        let found = MentionDiscovery::discover(root.path(), MentionLimits::default()).unwrap();

        let content = found.read_text("unicode.txt", 2).unwrap();
        assert_eq!(content.text, "a");
        assert!(content.truncated);
        assert!(matches!(
            found.read_text("../unicode.txt", 10),
            Err(MentionError::UnknownFile)
        ));
        assert!(matches!(
            found.read_text("binary", 10),
            Err(MentionError::BinaryFile)
        ));
        assert!(matches!(
            found.read_text("invalid-utf8", 10),
            Err(MentionError::BinaryFile)
        ));
        assert!(matches!(
            found.read_text("invalid-at-boundary", 4),
            Err(MentionError::BinaryFile)
        ));
    }

    #[test]
    fn mention_query_matching_and_replacement_follow_tui_rules() {
        assert_eq!(mention_query("read @src/ma"), Some(("src/ma", 5)));
        assert_eq!(mention_query("read\n@ré"), Some(("ré", 5)));
        assert_eq!(mention_query("read @file later"), None);
        assert_eq!(mention_query("read\r@file"), None);
        assert_eq!(mention_query("read\u{2003}@file"), None);
        let files = vec![
            "README.md".into(),
            "cmd/main.rs".into(),
            "src/main.rs".into(),
        ];
        assert_eq!(
            match_mentions(&files, "main", 10),
            ["cmd/main.rs", "src/main.rs"]
        );
        assert_eq!(
            replace_mention_token("read @RE", 5, "README.md").as_deref(),
            Some("read @README.md ")
        );
        assert!(replace_mention_token("read @RE", 5, "two words").is_none());
    }

    fn skill(name: &str, enabled: bool) -> SkillSpec {
        SkillSpec {
            name: name.into(),
            description: format!("{name} description"),
            enabled,
        }
    }

    #[test]
    fn skills_complete_only_enabled_prefixes_and_omit_selected_tokens() {
        let catalog = vec![
            skill("review", true),
            skill("release", true),
            skill("docs", true),
            skill("disabled", false),
        ];
        let completion = complete_skills(
            "Use $release with\u{2003}$re",
            &catalog,
            CompletionLimits::default(),
        )
        .unwrap();
        assert_eq!(completion.query, "re");
        assert_eq!(completion.token_start, "Use $release with\u{2003}".len());
        assert_eq!(
            completion
                .matches
                .iter()
                .map(|item| item.name.as_str())
                .collect::<Vec<_>>(),
            ["review"]
        );
        assert_eq!(
            replace_skill_token("Use $re", 4, "review").as_deref(),
            Some("Use $review ")
        );
        assert!(
            complete_skills("Use $re in prose", &catalog, CompletionLimits::default()).is_none()
        );
    }

    #[test]
    fn skill_completion_is_deduplicated_and_bounded() {
        let catalog = vec![
            skill("ccc", true),
            skill("aaa", true),
            skill("aaa", true),
            skill("bbb", true),
        ];
        let completion = complete_skills(
            "$",
            &catalog,
            CompletionLimits {
                max_catalog_items: 4,
                max_results: 2,
                max_result_bytes: usize::MAX,
            },
        )
        .unwrap();
        assert_eq!(
            completion
                .matches
                .iter()
                .map(|item| item.name.as_str())
                .collect::<Vec<_>>(),
            ["aaa", "bbb"]
        );
        assert!(completion.truncated);
    }

    fn command(name: &str, completion: CommandCompletion) -> SlashCommand {
        SlashCommand {
            name: name.into(),
            description: format!("{name} description"),
            completion,
        }
    }

    #[test]
    fn slash_selection_matches_navigates_wraps_and_dismisses() {
        let catalog = vec![
            command("/compact", CommandCompletion::Immediate),
            command("/model", CommandCompletion::Editable),
            command("/permissions", CommandCompletion::Editable),
        ];
        let mut state = SlashSelectionState::new(CompletionLimits::default());
        state.refresh("/", &catalog);
        assert!(state.visible);
        assert_eq!(state.matches.len(), 3);
        assert_eq!(
            state.handle_key(SlashKey::Up),
            SlashAction::SelectionChanged
        );
        assert_eq!(state.selected, 2);
        assert_eq!(
            state.handle_key(SlashKey::Down),
            SlashAction::SelectionChanged
        );
        assert_eq!(state.selected, 0);
        assert_eq!(state.handle_key(SlashKey::Escape), SlashAction::Dismissed);
        assert!(!state.visible);
        assert_eq!(state.handle_key(SlashKey::Enter), SlashAction::Ignored);
    }

    #[test]
    fn slash_selection_normalizes_after_filtering_shortens_results() {
        let catalog = vec![
            command("/compact", CommandCompletion::Immediate),
            command("/model", CommandCompletion::Editable),
            command("/permissions", CommandCompletion::Editable),
        ];
        let mut state = SlashSelectionState::new(CompletionLimits::default());
        state.refresh("/", &catalog);
        assert_eq!(
            state.handle_key(SlashKey::Up),
            SlashAction::SelectionChanged
        );
        assert_eq!(state.selected, 2);
        assert_eq!(
            state
                .selected_command()
                .map(|command| command.name.as_str()),
            Some("/permissions")
        );

        state.refresh("/mod", &catalog);
        assert_eq!(state.matches.len(), 1);
        assert_eq!(state.selected, 0);
        assert_eq!(
            state
                .selected_command()
                .map(|command| command.name.as_str()),
            Some("/model")
        );
    }

    #[test]
    fn slash_tab_inserts_while_enter_obeys_completion_metadata() {
        let catalog = vec![
            command("/compact", CommandCompletion::Immediate),
            command("/model", CommandCompletion::Editable),
        ];
        let mut state = SlashSelectionState::new(CompletionLimits::default());
        state.refresh("/mod", &catalog);
        assert_eq!(
            state.handle_key(SlashKey::Tab),
            SlashAction::Insert("/model ".into())
        );
        state.refresh("/mod", &catalog);
        assert_eq!(
            state.handle_key(SlashKey::Enter),
            SlashAction::Insert("/model ".into())
        );
        state.refresh("/com", &catalog);
        assert_eq!(
            state.handle_key(SlashKey::Enter),
            SlashAction::Execute("/compact".into())
        );
        state.refresh("/com", &catalog);
        assert_eq!(
            state.handle_key(SlashKey::Tab),
            SlashAction::Insert("/compact".into())
        );
        state.refresh("/model arg", &catalog);
        assert!(!state.visible);
    }

    #[test]
    fn slash_prefixes_precede_fuzzy_matches_and_results_are_bounded() {
        let catalog = vec![
            command("/camp", CommandCompletion::Immediate),
            command("/cmprefix", CommandCompletion::Immediate),
            command("/compact", CommandCompletion::Immediate),
        ];
        let mut state = SlashSelectionState::new(CompletionLimits {
            max_catalog_items: 3,
            max_results: 2,
            max_result_bytes: usize::MAX,
        });
        state.refresh("/cmp", &catalog);
        assert_eq!(state.matches[0].name, "/cmprefix");
        assert_eq!(state.matches.len(), 2);
        assert!(state.truncated);
    }

    #[test]
    fn default_paste_limits_leave_conservative_json_frame_headroom() {
        let limits = PasteLimits::default();
        assert_eq!(limits.max_single_bytes, 2 * 1024 * 1024);
        assert_eq!(limits.max_total_bytes, 2 * 1024 * 1024);
        assert_eq!(limits.max_expanded_bytes, 2 * 1024 * 1024);
        assert!(limits.max_expanded_bytes.saturating_mul(6) < 16 * 1024 * 1024);
    }

    #[test]
    fn paste_thresholds_are_inclusive_and_count_unicode_runes() {
        assert!(!PasteStore::should_collapse(
            &"é".repeat(LARGE_PASTE_RUNE_THRESHOLD - 1)
        ));
        assert!(PasteStore::should_collapse(
            &"é".repeat(LARGE_PASTE_RUNE_THRESHOLD)
        ));
        assert!(!PasteStore::should_collapse(
            &"x\n".repeat(LARGE_PASTE_LINE_THRESHOLD - 2)
        ));
        assert!(PasteStore::should_collapse(
            &"x\n".repeat(LARGE_PASTE_LINE_THRESHOLD - 1)
        ));
    }

    #[test]
    fn large_pastes_collapse_expand_remove_atomically_and_prune() {
        let mut store = PasteStore::default();
        assert_eq!(store.collapse("small".into()).unwrap(), None);
        let body = "body\n".repeat(50);
        let token = store.collapse(body.clone()).unwrap().unwrap();
        let mut compact = format!("before {token} after");
        assert_eq!(
            store.expand(&compact).unwrap(),
            format!("before {body} after")
        );
        let cursor = format!("before {token}").chars().count();
        assert!(store.remove_at_cursor(&mut compact, cursor, DeleteDirection::Backward));
        assert_eq!(compact, "before  after");
        assert!(store.attachments().is_empty());

        let token = store.collapse("x\n".repeat(40)).unwrap().unwrap();
        assert!(compact.is_empty() || !compact.contains(&token));
        store.prune(&compact);
        assert!(store.attachments().is_empty());
        assert_eq!(store.total_bytes(), 0);
    }

    #[test]
    fn paste_expansion_is_non_recursive_and_expands_each_token_once() {
        let first = CollapsedPaste {
            id: 1,
            token: "[one]".into(),
            text: "literal [two]".into(),
        };
        let second = CollapsedPaste {
            id: 2,
            token: "[two]".into(),
            text: "second".into(),
        };
        assert_eq!(
            expand_once("[one] [two] [one]", &[first, second], 100).unwrap(),
            "literal [two] second [one]"
        );
    }

    #[test]
    fn paste_store_enforces_all_state_and_expansion_bounds() {
        let limits = PasteLimits {
            max_attachments: 1,
            max_single_bytes: 500,
            max_total_bytes: 500,
            max_expanded_bytes: 300,
        };
        let mut store = PasteStore::new(limits);
        let body = "x\n".repeat(40);
        let token = store.collapse(body).unwrap().unwrap();
        assert!(matches!(
            store.collapse("y\n".repeat(40)),
            Err(PasteError::TooManyAttachments)
        ));
        assert!(matches!(
            store.expand(&format!("{token}{}", "z".repeat(300))),
            Err(PasteError::ExpansionTooLarge)
        ));

        let mut too_small = PasteStore::new(PasteLimits {
            max_attachments: 2,
            max_single_bytes: 10,
            max_total_bytes: 20,
            max_expanded_bytes: 100,
        });
        assert!(matches!(
            too_small.collapse("x\n".repeat(40)),
            Err(PasteError::PasteTooLarge)
        ));

        let mut aggregate = PasteStore::new(PasteLimits {
            max_attachments: 3,
            max_single_bytes: 100,
            max_total_bytes: 120,
            max_expanded_bytes: 500,
        });
        aggregate.collapse("a\n".repeat(40)).unwrap();
        assert!(matches!(
            aggregate.collapse("b\n".repeat(40)),
            Err(PasteError::TotalTooLarge)
        ));
    }

    #[test]
    fn rejected_submission_recovers_compact_tokens_and_exact_bodies() {
        let mut store = PasteStore::default();
        let body = "recover me\n".repeat(40);
        let token = store.collapse(body.clone()).unwrap().unwrap();
        let compact = format!("prefix {token}");
        let submission = store.prepare_submission(compact.clone()).unwrap();
        assert!(store.attachments().is_empty());
        assert_eq!(submission.compact_text(), compact);
        assert_eq!(submission.expanded_text(), format!("prefix {body}"));

        let restored = store.recover(submission).unwrap();
        assert_eq!(restored, compact);
        assert_eq!(store.expand(&restored).unwrap(), format!("prefix {body}"));
    }

    #[test]
    fn recovery_fails_closed_when_new_pastes_are_already_present() {
        let mut store = PasteStore::default();
        let token = store.collapse("old\n".repeat(40)).unwrap().unwrap();
        let submission = store.prepare_submission(token).unwrap();
        store.collapse("new\n".repeat(40)).unwrap();
        assert_eq!(store.recover(submission), Err(PasteError::InvalidRecovery));
    }
}
