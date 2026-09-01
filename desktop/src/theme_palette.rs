//! Bounded, dependency-light projection of the canonical `themes_list` RPC.
//!
//! The wire DTOs retain only the allowlisted, path-free theme fields. Projection
//! validates every bound before converting color strings into concrete RGB
//! values, so rendering never needs to retain arbitrary JSON or custom-theme
//! source text.

use std::{collections::HashSet, error::Error, fmt};

use serde::Deserialize;

pub(crate) const MAX_THEME_CATALOG_ITEMS: usize = 68;
pub(crate) const MAX_THEME_CATALOG_BYTES: usize = 128 * 1024;
pub(crate) const MAX_THEME_NAME_CHARS: usize = 64;
pub(crate) const MAX_THEME_NAME_BYTES: usize = 256;
pub(crate) const MAX_THEME_LABEL_CHARS: usize = 96;
pub(crate) const MAX_THEME_LABEL_BYTES: usize = 384;
pub(crate) const MAX_COLOR_SPEC_BYTES: usize = 7;
pub(crate) const MAX_PALETTE_DIAGNOSTIC_CHARS: usize = 256;
pub(crate) const MAX_PALETTE_DIAGNOSTIC_BYTES: usize = 1024;

const VISIBLE_BUILTINS: [(&str, &str); 4] = [
    ("default", "Snow"),
    ("frost", "Frost"),
    ("ember", "Ember"),
    ("aurora", "Aurora"),
];
const HIDDEN_LEGACY: [&str; 6] = [
    "dark",
    "light",
    "high-contrast",
    "nord",
    "dracula",
    "gruvbox",
];

/// The resolved native appearance. A caller must resolve any system preference
/// before choosing one half of an adaptive palette.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum NativeAppearance {
    Light,
    Dark,
}

/// An sRGB color with no GPUI or terminal dependency.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub(crate) struct Rgb {
    pub red: u8,
    pub green: u8,
    pub blue: u8,
}

impl Rgb {
    pub const fn new(red: u8, green: u8, blue: u8) -> Self {
        Self { red, green, blue }
    }
}

/// The seven semantic roles shared by Snow presentation surfaces.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub(crate) enum SemanticRole {
    Accent,
    Muted,
    Foreground,
    Warning,
    Error,
    Success,
    Separator,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) struct AdaptiveRgb {
    pub light: Rgb,
    pub dark: Rgb,
}

impl AdaptiveRgb {
    pub const fn resolve(self, appearance: NativeAppearance) -> Rgb {
        match appearance {
            NativeAppearance::Light => self.light,
            NativeAppearance::Dark => self.dark,
        }
    }
}

/// A fully resolved adaptive palette. No role or appearance half is optional.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) struct AdaptiveSemanticPalette {
    pub accent: AdaptiveRgb,
    pub muted: AdaptiveRgb,
    pub foreground: AdaptiveRgb,
    pub warning: AdaptiveRgb,
    pub error: AdaptiveRgb,
    pub success: AdaptiveRgb,
    pub separator: AdaptiveRgb,
}

impl AdaptiveSemanticPalette {
    pub const fn resolve(self, appearance: NativeAppearance) -> SemanticPalette {
        SemanticPalette {
            accent: self.accent.resolve(appearance),
            muted: self.muted.resolve(appearance),
            foreground: self.foreground.resolve(appearance),
            warning: self.warning.resolve(appearance),
            error: self.error.resolve(appearance),
            success: self.success.resolve(appearance),
            separator: self.separator.resolve(appearance),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) struct SemanticPalette {
    pub accent: Rgb,
    pub muted: Rgb,
    pub foreground: Rgb,
    pub warning: Rgb,
    pub error: Rgb,
    pub success: Rgb,
    pub separator: Rgb,
}

impl SemanticPalette {
    pub const fn color(self, role: SemanticRole) -> Rgb {
        match role {
            SemanticRole::Accent => self.accent,
            SemanticRole::Muted => self.muted,
            SemanticRole::Foreground => self.foreground,
            SemanticRole::Warning => self.warning,
            SemanticRole::Error => self.error,
            SemanticRole::Success => self.success,
            SemanticRole::Separator => self.separator,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum ThemeScope {
    Builtin,
    Global,
    Project,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum ThemeVisibility {
    Visible,
    HiddenLegacy,
}

/// A path-free, fully resolved descriptor safe for presentation.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct ThemeDescriptor {
    pub name: String,
    pub display_name: String,
    pub scope: ThemeScope,
    pub visibility: ThemeVisibility,
    pub colors: AdaptiveSemanticPalette,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct ActivePalette<'a> {
    pub requested_name: &'a str,
    pub descriptor: &'a ThemeDescriptor,
    pub colors: SemanticPalette,
}

/// A bounded, public-safe notice. Messages never include wire values.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct PaletteDiagnostic {
    message: String,
}

impl PaletteDiagnostic {
    fn new(message: &str) -> Self {
        Self {
            message: bounded_diagnostic(message),
        }
    }

    pub fn message(&self) -> &str {
        &self.message
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum ThemePaletteError {
    PayloadTooLarge,
    MalformedCatalog,
    TooManyThemes,
    InvalidSelection,
    InvalidThemeName,
    InvalidThemeLabel,
    InvalidThemeScope,
    InvalidColor,
    DuplicateTheme,
    ReservedThemeName,
    UnsupportedBuiltin,
    MissingBuiltin,
}

impl fmt::Display for ThemePaletteError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(match self {
            Self::PayloadTooLarge => "theme catalog exceeds the payload limit",
            Self::MalformedCatalog => "theme catalog is malformed",
            Self::TooManyThemes => "theme catalog exceeds the item limit",
            Self::InvalidSelection => "selected theme name is invalid",
            Self::InvalidThemeName => "theme name is invalid",
            Self::InvalidThemeLabel => "theme label is invalid",
            Self::InvalidThemeScope => "theme scope is invalid",
            Self::InvalidColor => "theme color is invalid",
            Self::DuplicateTheme => "theme catalog contains a duplicate name",
            Self::ReservedThemeName => "custom theme uses a reserved name",
            Self::UnsupportedBuiltin => "theme catalog contains an unsupported built-in",
            Self::MissingBuiltin => "theme catalog omits a required built-in",
        })
    }
}

impl Error for ThemePaletteError {}

/// Allowlisted wire shape for `themes_list`.
#[derive(Debug, Clone, Deserialize, PartialEq, Eq)]
pub(crate) struct ThemeCatalogDto {
    pub selected: String,
    pub themes: Vec<ThemeDescriptorDto>,
}

#[derive(Debug, Clone, Deserialize, PartialEq, Eq)]
pub(crate) struct ThemeDescriptorDto {
    pub name: String,
    pub display_name: String,
    pub scope: String,
    pub colors: ThemeColorsDto,
}

#[derive(Debug, Clone, Deserialize, PartialEq, Eq)]
pub(crate) struct ThemeColorsDto {
    pub accent: AdaptiveColorDto,
    pub muted: AdaptiveColorDto,
    pub foreground: AdaptiveColorDto,
    pub warning: AdaptiveColorDto,
    pub error: AdaptiveColorDto,
    pub success: AdaptiveColorDto,
    pub separator: AdaptiveColorDto,
}

#[derive(Debug, Clone, Deserialize, PartialEq, Eq)]
pub(crate) struct AdaptiveColorDto {
    pub light: String,
    pub dark: String,
}

/// Presentation state for the visible catalog and the selected active palette.
/// Hidden legacy descriptors may remain active but never appear in `themes()`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct ThemePaletteState {
    selected: String,
    themes: Vec<ThemeDescriptor>,
    hidden_legacy: Vec<ThemeDescriptor>,
    active: ThemeDescriptor,
    diagnostic: Option<PaletteDiagnostic>,
}

impl ThemePaletteState {
    pub fn decode_json(input: &str) -> Result<Self, ThemePaletteError> {
        if input.len() > MAX_THEME_CATALOG_BYTES {
            return Err(ThemePaletteError::PayloadTooLarge);
        }
        let dto = serde_json::from_str::<ThemeCatalogDto>(input)
            .map_err(|_| ThemePaletteError::MalformedCatalog)?;
        Self::from_dto(dto)
    }

    pub fn from_dto(dto: ThemeCatalogDto) -> Result<Self, ThemePaletteError> {
        validate_selection(&dto.selected)?;
        if dto.themes.len() > MAX_THEME_CATALOG_ITEMS {
            return Err(ThemePaletteError::TooManyThemes);
        }

        let mut names = HashSet::with_capacity(dto.themes.len());
        let mut themes = Vec::with_capacity(dto.themes.len());
        let mut hidden_legacy = Vec::new();
        for wire in dto.themes {
            if !names.insert(wire.name.clone()) {
                return Err(ThemePaletteError::DuplicateTheme);
            }
            let descriptor = project_descriptor(wire)?;
            match descriptor.visibility {
                ThemeVisibility::Visible => themes.push(descriptor),
                ThemeVisibility::HiddenLegacy => hidden_legacy.push(descriptor),
            }
        }
        for (name, _) in VISIBLE_BUILTINS {
            if !themes
                .iter()
                .any(|theme| theme.scope == ThemeScope::Builtin && theme.name == name)
            {
                return Err(ThemePaletteError::MissingBuiltin);
            }
        }

        let fallback = themes
            .iter()
            .find(|theme| theme.name == "default")
            .expect("required built-in checked above")
            .clone();
        let mut state = Self {
            selected: dto.selected,
            themes,
            hidden_legacy,
            active: fallback,
            diagnostic: None,
        };
        state.resolve_active();
        Ok(state)
    }

    pub fn selected_name(&self) -> &str {
        &self.selected
    }

    pub fn themes(&self) -> &[ThemeDescriptor] {
        &self.themes
    }

    pub fn active_descriptor(&self) -> &ThemeDescriptor {
        &self.active
    }

    pub fn diagnostic(&self) -> Option<&PaletteDiagnostic> {
        self.diagnostic.as_ref()
    }

    pub fn active_palette(&self, appearance: NativeAppearance) -> ActivePalette<'_> {
        ActivePalette {
            requested_name: &self.selected,
            descriptor: &self.active,
            colors: self.active.colors.resolve(appearance),
        }
    }

    /// Reconcile a settings update against the already bounded catalog.
    pub fn select(&mut self, selected: &str) -> Result<(), ThemePaletteError> {
        validate_selection(selected)?;
        self.selected.clear();
        self.selected.push_str(selected);
        self.resolve_active();
        Ok(())
    }

    fn resolve_active(&mut self) {
        self.diagnostic = None;
        if let Some(canonical) = legacy_alias(&self.selected) {
            self.active = self
                .themes
                .iter()
                .find(|theme| theme.name == canonical)
                .expect("legacy aliases target required built-ins")
                .clone();
            self.diagnostic = Some(PaletteDiagnostic::new(
                "A hidden legacy theme was mapped to its supported palette.",
            ));
            return;
        }
        if let Some(theme) = self.themes.iter().find(|theme| theme.name == self.selected) {
            self.active = theme.clone();
            return;
        }
        if self.selected == "high-contrast"
            && let Some(theme) = self
                .hidden_legacy
                .iter()
                .find(|theme| theme.name == self.selected)
        {
            self.active = theme.clone();
            self.diagnostic = Some(PaletteDiagnostic::new(
                "A hidden legacy theme remains active but is omitted from the picker.",
            ));
            return;
        }
        self.active = self
            .themes
            .iter()
            .find(|theme| theme.name == "default")
            .expect("required built-in checked during projection")
            .clone();
        self.diagnostic = Some(PaletteDiagnostic::new(
            "The selected theme is unavailable; Snow is using the default palette.",
        ));
    }
}

fn project_descriptor(wire: ThemeDescriptorDto) -> Result<ThemeDescriptor, ThemePaletteError> {
    validate_name(&wire.name).map_err(|_| ThemePaletteError::InvalidThemeName)?;
    validate_label(&wire.display_name)?;
    let scope = match wire.scope.as_str() {
        "builtin" => ThemeScope::Builtin,
        "global" => ThemeScope::Global,
        "project" => ThemeScope::Project,
        _ => return Err(ThemePaletteError::InvalidThemeScope),
    };

    let builtin = visible_builtin_label(&wire.name);
    let legacy = is_hidden_legacy(&wire.name);
    let (display_name, visibility) = match scope {
        ThemeScope::Builtin => {
            if let Some(label) = builtin {
                (label.to_owned(), ThemeVisibility::Visible)
            } else if legacy {
                (
                    legacy_label(&wire.name).to_owned(),
                    ThemeVisibility::HiddenLegacy,
                )
            } else {
                return Err(ThemePaletteError::UnsupportedBuiltin);
            }
        }
        ThemeScope::Global | ThemeScope::Project => {
            if builtin.is_some() || legacy {
                return Err(ThemePaletteError::ReservedThemeName);
            }
            (wire.display_name, ThemeVisibility::Visible)
        }
    };

    Ok(ThemeDescriptor {
        name: wire.name,
        display_name,
        scope,
        visibility,
        colors: project_colors(wire.colors)?,
    })
}

fn project_colors(wire: ThemeColorsDto) -> Result<AdaptiveSemanticPalette, ThemePaletteError> {
    Ok(AdaptiveSemanticPalette {
        accent: project_pair(wire.accent)?,
        muted: project_pair(wire.muted)?,
        foreground: project_pair(wire.foreground)?,
        warning: project_pair(wire.warning)?,
        error: project_pair(wire.error)?,
        success: project_pair(wire.success)?,
        separator: project_pair(wire.separator)?,
    })
}

fn project_pair(wire: AdaptiveColorDto) -> Result<AdaptiveRgb, ThemePaletteError> {
    Ok(AdaptiveRgb {
        light: decode_color(&wire.light)?,
        dark: decode_color(&wire.dark)?,
    })
}

pub(crate) fn decode_color(value: &str) -> Result<Rgb, ThemePaletteError> {
    if value.is_empty() || value.len() > MAX_COLOR_SPEC_BYTES || !value.is_ascii() {
        return Err(ThemePaletteError::InvalidColor);
    }
    if let Some(hex) = value.strip_prefix('#') {
        if hex.len() != 6 || !hex.bytes().all(|byte| byte.is_ascii_hexdigit()) {
            return Err(ThemePaletteError::InvalidColor);
        }
        return Ok(Rgb::new(
            decode_hex_byte(&hex[0..2])?,
            decode_hex_byte(&hex[2..4])?,
            decode_hex_byte(&hex[4..6])?,
        ));
    }
    if value.len() > 1 && value.starts_with('0') || !value.bytes().all(|byte| byte.is_ascii_digit())
    {
        return Err(ThemePaletteError::InvalidColor);
    }
    let index = value
        .parse::<u16>()
        .ok()
        .filter(|index| *index <= 255)
        .ok_or(ThemePaletteError::InvalidColor)?;
    Ok(xterm_256_rgb(index as u8))
}

fn decode_hex_byte(value: &str) -> Result<u8, ThemePaletteError> {
    u8::from_str_radix(value, 16).map_err(|_| ThemePaletteError::InvalidColor)
}

/// Deterministic xterm-256 decoding used for numeric terminal color specs.
pub(crate) const fn xterm_256_rgb(index: u8) -> Rgb {
    const ANSI: [Rgb; 16] = [
        Rgb::new(0x00, 0x00, 0x00),
        Rgb::new(0x80, 0x00, 0x00),
        Rgb::new(0x00, 0x80, 0x00),
        Rgb::new(0x80, 0x80, 0x00),
        Rgb::new(0x00, 0x00, 0x80),
        Rgb::new(0x80, 0x00, 0x80),
        Rgb::new(0x00, 0x80, 0x80),
        Rgb::new(0xc0, 0xc0, 0xc0),
        Rgb::new(0x80, 0x80, 0x80),
        Rgb::new(0xff, 0x00, 0x00),
        Rgb::new(0x00, 0xff, 0x00),
        Rgb::new(0xff, 0xff, 0x00),
        Rgb::new(0x00, 0x00, 0xff),
        Rgb::new(0xff, 0x00, 0xff),
        Rgb::new(0x00, 0xff, 0xff),
        Rgb::new(0xff, 0xff, 0xff),
    ];
    const CUBE: [u8; 6] = [0, 95, 135, 175, 215, 255];

    if index < 16 {
        return ANSI[index as usize];
    }
    if index < 232 {
        let offset = index - 16;
        return Rgb::new(
            CUBE[(offset / 36) as usize],
            CUBE[((offset % 36) / 6) as usize],
            CUBE[(offset % 6) as usize],
        );
    }
    let gray = 8 + (index - 232) * 10;
    Rgb::new(gray, gray, gray)
}

fn validate_selection(value: &str) -> Result<(), ThemePaletteError> {
    validate_name(value).map_err(|_| ThemePaletteError::InvalidSelection)
}

fn validate_name(value: &str) -> Result<(), ()> {
    if value.is_empty()
        || value.len() > MAX_THEME_NAME_BYTES
        || value.chars().count() > MAX_THEME_NAME_CHARS
        || value.chars().any(|character| {
            character < '\u{20}' || character == '\u{7f}' || matches!(character, '/' | '\\')
        })
    {
        return Err(());
    }
    Ok(())
}

fn validate_label(value: &str) -> Result<(), ThemePaletteError> {
    if value.is_empty()
        || value.len() > MAX_THEME_LABEL_BYTES
        || value.chars().count() > MAX_THEME_LABEL_CHARS
        || value
            .chars()
            .any(|character| character < '\u{20}' || character == '\u{7f}')
    {
        return Err(ThemePaletteError::InvalidThemeLabel);
    }
    Ok(())
}

fn visible_builtin_label(name: &str) -> Option<&'static str> {
    VISIBLE_BUILTINS
        .iter()
        .find_map(|(builtin, label)| (*builtin == name).then_some(*label))
}

fn is_hidden_legacy(name: &str) -> bool {
    HIDDEN_LEGACY.contains(&name)
}

fn legacy_alias(name: &str) -> Option<&'static str> {
    match name {
        "dark" | "light" => Some("default"),
        "nord" => Some("frost"),
        "gruvbox" => Some("ember"),
        "dracula" => Some("aurora"),
        _ => None,
    }
}

fn legacy_label(name: &str) -> &'static str {
    match name {
        "dark" => "Dark (legacy)",
        "light" => "Light (legacy)",
        "high-contrast" => "High Contrast (legacy)",
        "nord" => "Nord (legacy)",
        "dracula" => "Dracula (legacy)",
        "gruvbox" => "Gruvbox (legacy)",
        _ => "Legacy theme",
    }
}

fn bounded_diagnostic(value: &str) -> String {
    let mut output = String::with_capacity(value.len().min(MAX_PALETTE_DIAGNOSTIC_BYTES));
    for character in value.chars().take(MAX_PALETTE_DIAGNOSTIC_CHARS) {
        if output.len() + character.len_utf8() > MAX_PALETTE_DIAGNOSTIC_BYTES {
            break;
        }
        output.push(character);
    }
    output
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::{Value, json};

    fn pair(light: &str, dark: &str) -> Value {
        json!({"light": light, "dark": dark})
    }

    fn colors(light: &str, dark: &str) -> Value {
        json!({
            "accent": pair(light, dark),
            "muted": pair(light, dark),
            "foreground": pair(light, dark),
            "warning": pair(light, dark),
            "error": pair(light, dark),
            "success": pair(light, dark),
            "separator": pair(light, dark)
        })
    }

    fn descriptor(name: &str, label: &str, scope: &str, light: &str, dark: &str) -> Value {
        json!({
            "name": name,
            "display_name": label,
            "scope": scope,
            "colors": colors(light, dark)
        })
    }

    fn builtins() -> Vec<Value> {
        vec![
            descriptor(
                "default",
                "untrusted label",
                "builtin",
                "#010203",
                "#040506",
            ),
            descriptor("frost", "Frost", "builtin", "16", "231"),
            descriptor("ember", "Ember", "builtin", "#112233", "#445566"),
            descriptor("aurora", "Aurora", "builtin", "#778899", "#AABBCC"),
        ]
    }

    fn catalog(selected: &str, mut extra: Vec<Value>) -> String {
        let mut themes = builtins();
        themes.append(&mut extra);
        json!({"selected": selected, "themes": themes}).to_string()
    }

    #[test]
    fn hex_colors_decode_case_insensitively() {
        assert_eq!(decode_color("#00aF10"), Ok(Rgb::new(0x00, 0xaf, 0x10)));
        assert_eq!(decode_color("#FFFFFF"), Ok(Rgb::new(255, 255, 255)));
    }

    #[test]
    fn ansi_base_colors_match_xterm() {
        let expected = [
            (0, Rgb::new(0, 0, 0)),
            (1, Rgb::new(128, 0, 0)),
            (7, Rgb::new(192, 192, 192)),
            (8, Rgb::new(128, 128, 128)),
            (12, Rgb::new(0, 0, 255)),
            (15, Rgb::new(255, 255, 255)),
        ];
        for (index, color) in expected {
            assert_eq!(xterm_256_rgb(index), color);
            assert_eq!(decode_color(&index.to_string()), Ok(color));
        }
    }

    #[test]
    fn ansi_cube_and_grayscale_match_xterm() {
        let expected = [
            (16, Rgb::new(0, 0, 0)),
            (17, Rgb::new(0, 0, 95)),
            (21, Rgb::new(0, 0, 255)),
            (22, Rgb::new(0, 95, 0)),
            (51, Rgb::new(0, 255, 255)),
            (196, Rgb::new(255, 0, 0)),
            (231, Rgb::new(255, 255, 255)),
            (232, Rgb::new(8, 8, 8)),
            (233, Rgb::new(18, 18, 18)),
            (255, Rgb::new(238, 238, 238)),
        ];
        for (index, color) in expected {
            assert_eq!(xterm_256_rgb(index), color, "index {index}");
        }
    }

    #[test]
    fn color_syntax_is_strict_and_bounded() {
        for invalid in [
            "", "#fff", "#GG0000", "#0000000", " 1", "1 ", "+1", "-1", "01", "256", "9999999", "é",
        ] {
            assert_eq!(
                decode_color(invalid),
                Err(ThemePaletteError::InvalidColor),
                "{invalid:?}"
            );
        }
    }

    #[test]
    fn adaptive_palette_selects_every_role_from_explicit_appearance() {
        let state = ThemePaletteState::decode_json(&catalog("default", vec![])).unwrap();
        let light = state.active_palette(NativeAppearance::Light).colors;
        let dark = state.active_palette(NativeAppearance::Dark).colors;
        for role in [
            SemanticRole::Accent,
            SemanticRole::Muted,
            SemanticRole::Foreground,
            SemanticRole::Warning,
            SemanticRole::Error,
            SemanticRole::Success,
            SemanticRole::Separator,
        ] {
            assert_eq!(light.color(role), Rgb::new(1, 2, 3));
            assert_eq!(dark.color(role), Rgb::new(4, 5, 6));
        }
    }

    #[test]
    fn custom_global_and_project_themes_are_projected_path_free() {
        let input = catalog(
            "project glow",
            vec![
                descriptor("global glow", "Global Glow", "global", "33", "44"),
                descriptor("project glow", "Project Glow", "project", "45", "46"),
            ],
        );
        let state = ThemePaletteState::decode_json(&input).unwrap();
        assert_eq!(state.themes().len(), 6);
        assert_eq!(state.active_descriptor().name, "project glow");
        assert_eq!(state.active_descriptor().scope, ThemeScope::Project);
        assert_eq!(
            state.active_descriptor().visibility,
            ThemeVisibility::Visible
        );
        assert!(state.diagnostic().is_none());
        assert_eq!(
            state.active_palette(NativeAppearance::Light).colors.accent,
            xterm_256_rgb(45)
        );
    }

    #[test]
    fn built_in_labels_are_canonical_not_wire_controlled() {
        let state = ThemePaletteState::decode_json(&catalog("default", vec![])).unwrap();
        let labels = state
            .themes()
            .iter()
            .take(4)
            .map(|theme| theme.display_name.as_str())
            .collect::<Vec<_>>();
        assert_eq!(labels, vec!["Snow", "Frost", "Ember", "Aurora"]);
    }

    #[test]
    fn legacy_aliases_map_to_visible_canonical_palettes() {
        for (legacy, canonical) in [
            ("dark", "default"),
            ("light", "default"),
            ("nord", "frost"),
            ("gruvbox", "ember"),
            ("dracula", "aurora"),
        ] {
            let state = ThemePaletteState::decode_json(&catalog(legacy, vec![])).unwrap();
            assert_eq!(state.selected_name(), legacy);
            assert_eq!(state.active_descriptor().name, canonical);
            assert!(state.diagnostic().is_some());
        }
    }

    #[test]
    fn supplied_high_contrast_can_remain_active_but_stays_hidden() {
        let high_contrast = descriptor("high-contrast", "spoofed", "builtin", "#000000", "#FFFFFF");
        let state =
            ThemePaletteState::decode_json(&catalog("high-contrast", vec![high_contrast])).unwrap();
        assert_eq!(state.themes().len(), 4);
        assert_eq!(state.active_descriptor().name, "high-contrast");
        assert_eq!(
            state.active_descriptor().display_name,
            "High Contrast (legacy)"
        );
        assert_eq!(
            state.active_descriptor().visibility,
            ThemeVisibility::HiddenLegacy
        );
        assert!(state.diagnostic().is_some());
    }

    #[test]
    fn absent_high_contrast_and_unknown_selection_fall_back_safely() {
        for selected in ["high-contrast", "missing custom"] {
            let state = ThemePaletteState::decode_json(&catalog(selected, vec![])).unwrap();
            assert_eq!(state.active_descriptor().name, "default");
            let diagnostic = state.diagnostic().unwrap().message();
            assert!(diagnostic.len() <= MAX_PALETTE_DIAGNOSTIC_BYTES);
            assert!(!diagnostic.contains(selected));
        }
    }

    #[test]
    fn all_hidden_legacy_descriptors_are_omitted_from_picker_projection() {
        let hidden = HIDDEN_LEGACY
            .iter()
            .map(|name| descriptor(name, name, "builtin", "1", "2"))
            .collect();
        let state = ThemePaletteState::decode_json(&catalog("default", hidden)).unwrap();
        assert_eq!(state.themes().len(), 4);
        assert!(
            state
                .themes()
                .iter()
                .all(|theme| theme.visibility == ThemeVisibility::Visible)
        );
    }

    #[test]
    fn duplicate_names_are_rejected_before_projection() {
        let duplicate = descriptor("default", "Snow", "builtin", "1", "2");
        assert_eq!(
            ThemePaletteState::decode_json(&catalog("default", vec![duplicate])),
            Err(ThemePaletteError::DuplicateTheme)
        );
    }

    #[test]
    fn missing_required_builtin_is_rejected() {
        let mut value: Value = serde_json::from_str(&catalog("default", vec![])).unwrap();
        value["themes"]
            .as_array_mut()
            .unwrap()
            .retain(|theme| theme["name"] != "frost");
        assert_eq!(
            ThemePaletteState::decode_json(&value.to_string()),
            Err(ThemePaletteError::MissingBuiltin)
        );
    }

    #[test]
    fn custom_themes_cannot_claim_reserved_or_legacy_names() {
        for name in ["default", "aurora", "nord", "high-contrast"] {
            assert_eq!(
                ThemePaletteState::decode_json(&catalog(
                    "default",
                    vec![descriptor(name, name, "project", "1", "2")],
                )),
                if matches!(name, "default" | "aurora") {
                    Err(ThemePaletteError::DuplicateTheme)
                } else {
                    Err(ThemePaletteError::ReservedThemeName)
                }
            );
        }
    }

    #[test]
    fn unsupported_builtin_and_scope_are_rejected() {
        assert_eq!(
            ThemePaletteState::decode_json(&catalog(
                "default",
                vec![descriptor("future", "Future", "builtin", "1", "2")],
            )),
            Err(ThemePaletteError::UnsupportedBuiltin)
        );
        assert_eq!(
            ThemePaletteState::decode_json(&catalog(
                "default",
                vec![descriptor("custom", "Custom", "workspace", "1", "2")],
            )),
            Err(ThemePaletteError::InvalidThemeScope)
        );
    }

    #[test]
    fn catalog_and_payload_bounds_are_enforced() {
        let extras = (0..(MAX_THEME_CATALOG_ITEMS - 3))
            .map(|index| descriptor(&format!("custom-{index}"), "Custom", "global", "1", "2"))
            .collect();
        assert_eq!(
            ThemePaletteState::decode_json(&catalog("default", extras)),
            Err(ThemePaletteError::TooManyThemes)
        );
        let oversized = " ".repeat(MAX_THEME_CATALOG_BYTES + 1);
        assert_eq!(
            ThemePaletteState::decode_json(&oversized),
            Err(ThemePaletteError::PayloadTooLarge)
        );
    }

    #[test]
    fn name_selection_and_label_bounds_are_strict() {
        let too_long_name = "x".repeat(MAX_THEME_NAME_CHARS + 1);
        assert_eq!(
            ThemePaletteState::decode_json(&catalog(
                "default",
                vec![descriptor(&too_long_name, "Custom", "global", "1", "2")],
            )),
            Err(ThemePaletteError::InvalidThemeName)
        );
        let too_long_label = "x".repeat(MAX_THEME_LABEL_CHARS + 1);
        assert_eq!(
            ThemePaletteState::decode_json(&catalog(
                "default",
                vec![descriptor("custom", &too_long_label, "global", "1", "2")],
            )),
            Err(ThemePaletteError::InvalidThemeLabel)
        );
        for selected in ["", "bad/name", "bad\nname"] {
            assert_eq!(
                ThemePaletteState::decode_json(&catalog(selected, vec![])),
                Err(ThemePaletteError::InvalidSelection)
            );
        }
    }

    #[test]
    fn every_color_half_and_role_is_required_and_valid() {
        let mut malformed: Value = serde_json::from_str(&catalog("default", vec![])).unwrap();
        malformed["themes"][0]["colors"]["warning"]["dark"] = json!("#oops!!");
        assert_eq!(
            ThemePaletteState::decode_json(&malformed.to_string()),
            Err(ThemePaletteError::InvalidColor)
        );
        malformed["themes"][0]["colors"]["warning"]
            .as_object_mut()
            .unwrap()
            .remove("dark");
        assert_eq!(
            ThemePaletteState::decode_json(&malformed.to_string()),
            Err(ThemePaletteError::MalformedCatalog)
        );
    }

    #[test]
    fn unknown_fields_are_not_retained_or_rendered() {
        let mut value: Value = serde_json::from_str(&catalog("default", vec![])).unwrap();
        value["provider_data"] = json!({"secret": "do not retain"});
        value["themes"][0]["path"] = json!("/private/theme.yaml");
        let state = ThemePaletteState::decode_json(&value.to_string()).unwrap();
        assert_eq!(state.themes()[0].name, "default");
        assert_eq!(state.themes()[0].display_name, "Snow");
    }

    #[test]
    fn selection_can_be_reconciled_without_redecoding_catalog() {
        let mut state = ThemePaletteState::decode_json(&catalog("default", vec![])).unwrap();
        state.select("frost").unwrap();
        assert_eq!(state.selected_name(), "frost");
        assert_eq!(state.active_descriptor().name, "frost");
        assert!(state.diagnostic().is_none());
        state.select("nord").unwrap();
        assert_eq!(state.active_descriptor().name, "frost");
        assert!(state.diagnostic().is_some());
        assert_eq!(
            state.select("bad/path"),
            Err(ThemePaletteError::InvalidSelection)
        );
        assert_eq!(state.selected_name(), "nord");
    }

    #[test]
    fn diagnostics_are_utf8_safe_and_bounded() {
        let input = "🧊".repeat(MAX_PALETTE_DIAGNOSTIC_CHARS + 20);
        let diagnostic = PaletteDiagnostic::new(&input);
        assert_eq!(
            diagnostic.message().chars().count(),
            MAX_PALETTE_DIAGNOSTIC_CHARS
        );
        assert!(diagnostic.message().len() <= MAX_PALETTE_DIAGNOSTIC_BYTES);
        assert!(std::str::from_utf8(diagnostic.message().as_bytes()).is_ok());
    }

    #[test]
    fn malformed_json_is_reported_without_parser_text() {
        assert_eq!(
            ThemePaletteState::decode_json("{\"selected\":\"secret"),
            Err(ThemePaletteError::MalformedCatalog)
        );
        assert_eq!(
            ThemePaletteError::MalformedCatalog.to_string(),
            "theme catalog is malformed"
        );
    }
}
