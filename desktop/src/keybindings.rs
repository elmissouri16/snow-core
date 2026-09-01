//! Semantic desktop keybindings and GPUI binding specifications.
//!
//! Persisted shortcuts deliberately use Snow's portable `ctrl+up` spelling.
//! GPUI uses a different spelling (`ctrl-up`, `escape`, `pageup`, and so on),
//! so conversion happens only when binding specifications are built. GPUI can
//! replace layered bindings at runtime by installing `NoAction` for the old
//! bindings before installing the new action bindings; [`plan_change`] exposes
//! that ordered two-phase data without defining or dispatching GPUI actions.

use std::{
    collections::{BTreeMap, BTreeSet},
    error::Error,
    fmt,
};

/// A mutually exclusive interaction state used for collision validation.
///
/// The GPUI selector is intentionally stable: startup code and future action
/// handlers must use the same selector when installing and dispatching a spec.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum BindingContext {
    ComposerIdle,
    ComposerBusy,
    WorkspaceIdle,
    Settings,
    Transcript,
    Picker,
}

impl BindingContext {
    pub const ALL: [Self; 6] = [
        Self::ComposerIdle,
        Self::ComposerBusy,
        Self::WorkspaceIdle,
        Self::Settings,
        Self::Transcript,
        Self::Picker,
    ];

    pub const fn stable_name(self) -> &'static str {
        match self {
            Self::ComposerIdle => "composer_idle",
            Self::ComposerBusy => "composer_busy",
            Self::WorkspaceIdle => "workspace_idle",
            Self::Settings => "settings",
            Self::Transcript => "transcript",
            Self::Picker => "picker",
        }
    }

    pub const fn gpui_selector(self) -> &'static str {
        match self {
            Self::ComposerIdle => "DesktopComposerIdle",
            Self::ComposerBusy => "DesktopComposerBusy",
            Self::WorkspaceIdle => "DesktopWorkspaceIdle",
            Self::Settings => "DesktopSettings",
            Self::Transcript => "DesktopTranscript",
            Self::Picker => "DesktopPicker",
        }
    }
}

/// User-facing grouping for shortcut catalogs and help.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum ShortcutGroup {
    Composer,
    Global,
    Transcript,
    Pickers,
    Branches,
}

impl ShortcutGroup {
    pub const fn label(self) -> &'static str {
        match self {
            Self::Composer => "Composer",
            Self::Global => "Global",
            Self::Transcript => "Transcript",
            Self::Pickers => "Pickers",
            Self::Branches => "Branches",
        }
    }
}

/// One stable semantic action and its built-in bindings.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ActionDefinition {
    pub name: &'static str,
    pub label: &'static str,
    pub group: ShortcutGroup,
    pub defaults: &'static [&'static str],
    pub contexts: &'static [BindingContext],
}

const IDLE_COMPOSER: &[BindingContext] = &[BindingContext::ComposerIdle];
const BUSY_COMPOSER: &[BindingContext] = &[BindingContext::ComposerBusy];
const IDLE_AND_BUSY_COMPOSER: &[BindingContext] =
    &[BindingContext::ComposerIdle, BindingContext::ComposerBusy];
const THINKING_STATES: &[BindingContext] = &[
    BindingContext::ComposerIdle,
    BindingContext::ComposerBusy,
    BindingContext::Settings,
];
const GLOBAL_STATES: &[BindingContext] = &[
    BindingContext::ComposerIdle,
    BindingContext::ComposerBusy,
    BindingContext::WorkspaceIdle,
    BindingContext::Picker,
];
const MODEL_STATES: &[BindingContext] = &[
    BindingContext::ComposerIdle,
    BindingContext::ComposerBusy,
    BindingContext::WorkspaceIdle,
    BindingContext::Settings,
    BindingContext::Picker,
];
const WORKSPACE_IDLE: &[BindingContext] = &[BindingContext::WorkspaceIdle];
const TRANSCRIPT: &[BindingContext] = &[BindingContext::Transcript];
const PICKER: &[BindingContext] = &[BindingContext::Picker];

/// Complete configurable TUI semantic action inventory applicable to desktop.
///
/// Names and defaults are persistence contracts. Additions should be appended
/// within their semantic group; existing names must not be renamed or reused.
pub const ACTION_INVENTORY: &[ActionDefinition] = &[
    ActionDefinition {
        name: "submit",
        label: "Submit",
        group: ShortcutGroup::Composer,
        defaults: &["enter"],
        contexts: IDLE_AND_BUSY_COMPOSER,
    },
    ActionDefinition {
        name: "follow_up",
        label: "Follow-up",
        group: ShortcutGroup::Composer,
        defaults: &["alt+enter"],
        contexts: BUSY_COMPOSER,
    },
    ActionDefinition {
        name: "newline",
        label: "Newline",
        group: ShortcutGroup::Composer,
        defaults: &["ctrl+j", "alt+enter"],
        contexts: IDLE_COMPOSER,
    },
    ActionDefinition {
        name: "paste",
        label: "Paste",
        group: ShortcutGroup::Composer,
        defaults: &["ctrl+v"],
        contexts: IDLE_COMPOSER,
    },
    ActionDefinition {
        name: "abort",
        label: "Abort",
        group: ShortcutGroup::Composer,
        defaults: &["ctrl+c", "esc"],
        contexts: IDLE_AND_BUSY_COMPOSER,
    },
    ActionDefinition {
        name: "quit",
        label: "Quit",
        group: ShortcutGroup::Composer,
        defaults: &["ctrl+c", "ctrl+d"],
        contexts: WORKSPACE_IDLE,
    },
    ActionDefinition {
        name: "toggle_mode",
        label: "Toggle mode",
        group: ShortcutGroup::Composer,
        defaults: &["shift+tab"],
        contexts: IDLE_COMPOSER,
    },
    ActionDefinition {
        name: "thinking",
        label: "Thinking effort",
        group: ShortcutGroup::Composer,
        defaults: &["ctrl+t"],
        contexts: THINKING_STATES,
    },
    ActionDefinition {
        name: "models",
        label: "Models",
        group: ShortcutGroup::Global,
        defaults: &["alt+m"],
        contexts: MODEL_STATES,
    },
    ActionDefinition {
        name: "agents",
        label: "Agent fleet",
        group: ShortcutGroup::Global,
        defaults: &["alt+a"],
        contexts: GLOBAL_STATES,
    },
    ActionDefinition {
        name: "processes",
        label: "Process fleet",
        group: ShortcutGroup::Global,
        defaults: &["alt+p"],
        contexts: GLOBAL_STATES,
    },
    ActionDefinition {
        name: "page_up",
        label: "Page up",
        group: ShortcutGroup::Transcript,
        defaults: &["pgup"],
        contexts: TRANSCRIPT,
    },
    ActionDefinition {
        name: "page_down",
        label: "Page down",
        group: ShortcutGroup::Transcript,
        defaults: &["pgdown"],
        contexts: TRANSCRIPT,
    },
    ActionDefinition {
        name: "top",
        label: "Top",
        group: ShortcutGroup::Transcript,
        defaults: &["home"],
        contexts: TRANSCRIPT,
    },
    ActionDefinition {
        name: "bottom",
        label: "Bottom",
        group: ShortcutGroup::Transcript,
        defaults: &["end"],
        contexts: TRANSCRIPT,
    },
    ActionDefinition {
        name: "line_up",
        label: "Line up",
        group: ShortcutGroup::Transcript,
        defaults: &["ctrl+up"],
        contexts: TRANSCRIPT,
    },
    ActionDefinition {
        name: "line_down",
        label: "Line down",
        group: ShortcutGroup::Transcript,
        defaults: &["ctrl+down"],
        contexts: TRANSCRIPT,
    },
    ActionDefinition {
        name: "picker_up",
        label: "Previous item",
        group: ShortcutGroup::Pickers,
        defaults: &["up", "left", "k"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "picker_down",
        label: "Next item",
        group: ShortcutGroup::Pickers,
        defaults: &["down", "right", "j"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "picker_previous",
        label: "Previous field",
        group: ShortcutGroup::Pickers,
        defaults: &["shift+tab"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "picker_next",
        label: "Next field",
        group: ShortcutGroup::Pickers,
        defaults: &["tab"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "picker_page_up",
        label: "Picker page up",
        group: ShortcutGroup::Pickers,
        defaults: &["pgup"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "picker_page_down",
        label: "Picker page down",
        group: ShortcutGroup::Pickers,
        defaults: &["pgdown"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "picker_top",
        label: "First item",
        group: ShortcutGroup::Pickers,
        defaults: &["home"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "picker_bottom",
        label: "Last item",
        group: ShortcutGroup::Pickers,
        defaults: &["end"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "accept",
        label: "Accept",
        group: ShortcutGroup::Pickers,
        defaults: &["enter"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "close",
        label: "Close",
        group: ShortcutGroup::Pickers,
        defaults: &["esc"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "branch_fork",
        label: "Fork branch",
        group: ShortcutGroup::Branches,
        defaults: &["f"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "branch_rename",
        label: "Rename branch",
        group: ShortcutGroup::Branches,
        defaults: &["r"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "branch_delete",
        label: "Delete branch",
        group: ShortcutGroup::Branches,
        defaults: &["d"],
        contexts: PICKER,
    },
    ActionDefinition {
        name: "confirm",
        label: "Confirm",
        group: ShortcutGroup::Branches,
        defaults: &["y"],
        contexts: PICKER,
    },
];

/// A validated shortcut in canonical persisted form.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct CanonicalShortcut(String);

impl CanonicalShortcut {
    pub fn parse(value: &str) -> Result<Self, ShortcutParseError> {
        let mut canonical = value.trim().to_lowercase();
        if canonical == "escape" {
            canonical = "esc".to_owned();
        }
        if canonical.is_empty() {
            return Err(ShortcutParseError::new(value, "shortcut cannot be empty"));
        }
        if canonical.chars().any(char::is_whitespace) {
            return Err(ShortcutParseError::new(
                value,
                "whitespace is not allowed inside a shortcut",
            ));
        }
        if canonical.contains('-') {
            return Err(ShortcutParseError::new(
                value,
                "use portable '+' modifiers, for example ctrl+up",
            ));
        }

        let parts: Vec<&str> = canonical.split('+').collect();
        let valid = match parts.as_slice() {
            [base] => valid_unmodified_base(base),
            [modifier, base] if *modifier == "ctrl" || *modifier == "alt" => {
                valid_modified_base(base)
            }
            [modifier, base] if *modifier == "shift" => *base == "tab",
            _ => false,
        };
        if !valid {
            return Err(ShortcutParseError::new(
                value,
                "unsupported key name or modifier combination",
            ));
        }
        Ok(Self(canonical))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }

    /// Translate canonical persisted syntax to GPUI's keystroke syntax.
    pub fn gpui_keystroke(&self) -> String {
        let mut parts: Vec<&str> = self.0.split('+').collect();
        let base = parts.pop().expect("validated shortcut has a base key");
        let gpui_base = match base {
            "esc" => "escape",
            "pgup" => "pageup",
            "pgdown" => "pagedown",
            other => other,
        };
        if parts.is_empty() {
            gpui_base.to_owned()
        } else {
            format!("{}-{gpui_base}", parts.join("-"))
        }
    }
}

impl fmt::Display for CanonicalShortcut {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.as_str())
    }
}

fn valid_unmodified_base(base: &str) -> bool {
    const NAMED: &[&str] = &[
        "enter",
        "esc",
        "tab",
        "up",
        "down",
        "left",
        "right",
        "home",
        "end",
        "pgup",
        "pgdown",
        "backspace",
        "delete",
    ];
    NAMED.contains(&base) || valid_single_character(base)
}

fn valid_modified_base(base: &str) -> bool {
    matches!(base, "enter" | "up" | "down") || valid_single_character(base)
}

fn valid_single_character(value: &str) -> bool {
    let mut characters = value.chars();
    matches!(characters.next(), Some(character) if !character.is_control() && !matches!(character, '+' | '-'))
        && characters.next().is_none()
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ShortcutParseError {
    pub value: String,
    pub reason: &'static str,
}

impl ShortcutParseError {
    fn new(value: &str, reason: &'static str) -> Self {
        Self {
            value: value.to_owned(),
            reason,
        }
    }
}

impl fmt::Display for ShortcutParseError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            formatter,
            "invalid shortcut {:?}: {}",
            self.value, self.reason
        )
    }
}

impl Error for ShortcutParseError {}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BindingConfigurationError {
    UnknownAction(String),
    InvalidShortcut {
        action: String,
        source: ShortcutParseError,
    },
    InvalidInventory(String),
    Collision {
        context: BindingContext,
        shortcut: String,
        first_action: &'static str,
        second_action: &'static str,
    },
}

impl fmt::Display for BindingConfigurationError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::UnknownAction(action) => {
                write!(formatter, "unknown keybinding action {action:?}")
            }
            Self::InvalidShortcut { action, source } => {
                write!(formatter, "invalid keybinding for {action:?}: {source}")
            }
            Self::InvalidInventory(reason) => {
                write!(formatter, "invalid built-in keybinding inventory: {reason}")
            }
            Self::Collision {
                context,
                shortcut,
                first_action,
                second_action,
            } => write!(
                formatter,
                "key {shortcut:?} collides between {first_action} and {second_action} in {}",
                context.stable_name()
            ),
        }
    }
}

impl Error for BindingConfigurationError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        match self {
            Self::InvalidShortcut { source, .. } => Some(source),
            _ => None,
        }
    }
}

/// One deterministic, user-facing effective shortcut catalog row.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EffectiveShortcut {
    pub action: &'static str,
    pub label: &'static str,
    pub group: ShortcutGroup,
    pub shortcuts: Vec<String>,
    pub overridden: bool,
}

/// A GPUI-agnostic action-binding instruction. Startup or runtime wiring can
/// turn each item into a `gpui::KeyBinding` after registering its action type.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StartupBindingSpec {
    pub action: &'static str,
    pub context: BindingContext,
    pub gpui_context: &'static str,
    pub canonical_shortcut: String,
    pub gpui_keystroke: String,
}

/// Effective, validated bindings used to generate initial or replacement specs.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StartupBindingConfiguration {
    normalized_overrides: BTreeMap<String, Vec<String>>,
    catalog: Vec<EffectiveShortcut>,
}

impl StartupBindingConfiguration {
    pub fn from_overrides(
        overrides: &BTreeMap<String, Vec<String>>,
    ) -> Result<Self, BindingConfigurationError> {
        validate_inventory()?;
        let mut normalized_overrides = BTreeMap::new();
        let mut effective = BTreeMap::<&'static str, Vec<CanonicalShortcut>>::new();

        for definition in ACTION_INVENTORY {
            let defaults = definition
                .defaults
                .iter()
                .map(|value| {
                    CanonicalShortcut::parse(value).map_err(|source| {
                        BindingConfigurationError::InvalidInventory(format!(
                            "{} has invalid default {value:?}: {source}",
                            definition.name
                        ))
                    })
                })
                .collect::<Result<Vec<_>, _>>()?;
            effective.insert(definition.name, defaults);
        }

        for (action, values) in overrides {
            let definition = action_definition(action)
                .ok_or_else(|| BindingConfigurationError::UnknownAction(action.clone()))?;
            let mut seen = BTreeSet::new();
            let mut shortcuts = Vec::with_capacity(values.len());
            for value in values {
                let shortcut = CanonicalShortcut::parse(value).map_err(|source| {
                    BindingConfigurationError::InvalidShortcut {
                        action: action.clone(),
                        source,
                    }
                })?;
                if seen.insert(shortcut.clone()) {
                    shortcuts.push(shortcut);
                }
            }
            normalized_overrides.insert(
                action.clone(),
                shortcuts
                    .iter()
                    .map(|shortcut| shortcut.as_str().to_owned())
                    .collect(),
            );
            effective.insert(definition.name, shortcuts);
        }

        retain_emergency_shortcuts(&mut effective)?;
        validate_collisions(&effective)?;

        let catalog = ACTION_INVENTORY
            .iter()
            .map(|definition| EffectiveShortcut {
                action: definition.name,
                label: definition.label,
                group: definition.group,
                shortcuts: effective[definition.name]
                    .iter()
                    .map(|shortcut| shortcut.as_str().to_owned())
                    .collect(),
                overridden: normalized_overrides.contains_key(definition.name),
            })
            .collect();

        Ok(Self {
            normalized_overrides,
            catalog,
        })
    }

    pub fn normalized_overrides(&self) -> &BTreeMap<String, Vec<String>> {
        &self.normalized_overrides
    }

    pub fn catalog(&self) -> &[EffectiveShortcut] {
        &self.catalog
    }

    pub fn effective_shortcuts(&self, action: &str) -> Option<&[String]> {
        self.catalog
            .iter()
            .find(|entry| entry.action == action)
            .map(|entry| entry.shortcuts.as_slice())
    }

    /// Generate stable action specs in inventory, context, then shortcut order.
    /// No GPUI action value is created by this pure configuration layer.
    pub fn startup_specs(&self) -> Vec<StartupBindingSpec> {
        let mut specs = Vec::new();
        for (definition, entry) in ACTION_INVENTORY.iter().zip(&self.catalog) {
            for context in definition.contexts {
                for shortcut in &entry.shortcuts {
                    let shortcut = CanonicalShortcut::parse(shortcut)
                        .expect("effective catalog only contains validated shortcuts");
                    specs.push(StartupBindingSpec {
                        action: definition.name,
                        context: *context,
                        gpui_context: context.gpui_selector(),
                        canonical_shortcut: shortcut.as_str().to_owned(),
                        gpui_keystroke: shortcut.gpui_keystroke(),
                    });
                }
            }
        }
        specs
    }

    /// Deterministic help suitable for a settings panel or `/keybindings` view.
    pub fn help_text(&self) -> String {
        let mut output = String::from("Keyboard shortcuts\n");
        let groups = [
            ShortcutGroup::Composer,
            ShortcutGroup::Global,
            ShortcutGroup::Transcript,
            ShortcutGroup::Pickers,
            ShortcutGroup::Branches,
        ];
        for group in groups {
            output.push('\n');
            output.push_str(group.label());
            output.push('\n');
            for entry in self.catalog.iter().filter(|entry| entry.group == group) {
                output.push_str("  ");
                output.push_str(entry.label);
                output.push_str(": ");
                if entry.shortcuts.is_empty() {
                    output.push_str("unbound");
                } else {
                    output.push_str(&entry.shortcuts.join(" / "));
                }
                output.push('\n');
            }
        }
        output.push_str("\nShortcut changes can be applied without restarting.\n");
        output
    }
}

impl Default for StartupBindingConfiguration {
    fn default() -> Self {
        Self::from_overrides(&BTreeMap::new()).expect("built-in keybindings must be valid")
    }
}

/// One old binding to replace with GPUI `NoAction`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NoActionBindingSpec {
    pub previous_action: &'static str,
    pub context: BindingContext,
    pub gpui_context: &'static str,
    pub canonical_shortcut: String,
    pub gpui_keystroke: String,
}

/// Ordered two-phase data for replacing layered GPUI bindings at runtime.
///
/// Consumers must install every `no_action_specs` entry first, using GPUI's
/// `NoAction`, and then install `action_specs` in their listed order.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct RuntimeBindingPlan {
    pub no_action_specs: Vec<NoActionBindingSpec>,
    pub action_specs: Vec<StartupBindingSpec>,
}

impl RuntimeBindingPlan {
    pub fn is_empty(&self) -> bool {
        self.no_action_specs.is_empty() && self.action_specs.is_empty()
    }
}

/// Whether a validated preference update changes effective GPUI bindings.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BindingChangeEffect {
    Unchanged,
    RuntimeApplicable,
}

/// Result of evaluating persisted overrides against the active binding set.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BindingChange {
    pub effect: BindingChangeEffect,
    pub next: StartupBindingConfiguration,
    pub runtime_plan: RuntimeBindingPlan,
}

impl BindingChange {
    pub const fn can_apply_runtime(&self) -> bool {
        matches!(self.effect, BindingChangeEffect::RuntimeApplicable)
    }
}

/// Validate overrides and build a deterministic layered GPUI replacement plan.
pub fn plan_change(
    current: &StartupBindingConfiguration,
    overrides: &BTreeMap<String, Vec<String>>,
) -> Result<BindingChange, BindingConfigurationError> {
    let next = StartupBindingConfiguration::from_overrides(overrides)?;
    let old_specs = current.startup_specs();
    let new_specs = next.startup_specs();
    let (effect, runtime_plan) = if old_specs == new_specs {
        (
            BindingChangeEffect::Unchanged,
            RuntimeBindingPlan::default(),
        )
    } else {
        let no_action_specs = old_specs
            .into_iter()
            .map(|spec| NoActionBindingSpec {
                previous_action: spec.action,
                context: spec.context,
                gpui_context: spec.gpui_context,
                canonical_shortcut: spec.canonical_shortcut,
                gpui_keystroke: spec.gpui_keystroke,
            })
            .collect();
        (
            BindingChangeEffect::RuntimeApplicable,
            RuntimeBindingPlan {
                no_action_specs,
                action_specs: new_specs,
            },
        )
    };
    Ok(BindingChange {
        effect,
        next,
        runtime_plan,
    })
}

pub fn action_definition(name: &str) -> Option<&'static ActionDefinition> {
    ACTION_INVENTORY
        .iter()
        .find(|definition| definition.name == name)
}

fn validate_inventory() -> Result<(), BindingConfigurationError> {
    let mut names = BTreeSet::new();
    for definition in ACTION_INVENTORY {
        if !names.insert(definition.name) {
            return Err(BindingConfigurationError::InvalidInventory(format!(
                "duplicate action {:?}",
                definition.name
            )));
        }
        if definition.defaults.is_empty() {
            return Err(BindingConfigurationError::InvalidInventory(format!(
                "action {:?} has no default",
                definition.name
            )));
        }
        if definition.contexts.is_empty() {
            return Err(BindingConfigurationError::InvalidInventory(format!(
                "action {:?} has no context",
                definition.name
            )));
        }
    }
    Ok(())
}

fn retain_emergency_shortcuts(
    effective: &mut BTreeMap<&'static str, Vec<CanonicalShortcut>>,
) -> Result<(), BindingConfigurationError> {
    for (action, required) in [
        ("abort", &["ctrl+c", "esc"][..]),
        ("quit", &["ctrl+c"][..]),
        ("close", &["esc"][..]),
    ] {
        let shortcuts = effective.get_mut(action).ok_or_else(|| {
            BindingConfigurationError::InvalidInventory(format!(
                "missing mandatory emergency action {action:?}"
            ))
        })?;
        for value in required {
            let shortcut = CanonicalShortcut::parse(value).map_err(|source| {
                BindingConfigurationError::InvalidInventory(format!(
                    "invalid emergency shortcut for {action}: {source}"
                ))
            })?;
            if !shortcuts.contains(&shortcut) {
                shortcuts.push(shortcut);
            }
        }
    }
    Ok(())
}

fn validate_collisions(
    effective: &BTreeMap<&'static str, Vec<CanonicalShortcut>>,
) -> Result<(), BindingConfigurationError> {
    for context in BindingContext::ALL {
        let mut seen = BTreeMap::<String, &'static str>::new();
        for definition in ACTION_INVENTORY
            .iter()
            .filter(|definition| definition.contexts.contains(&context))
        {
            for shortcut in &effective[definition.name] {
                if let Some(first_action) =
                    seen.insert(shortcut.as_str().to_owned(), definition.name)
                {
                    return Err(BindingConfigurationError::Collision {
                        context,
                        shortcut: shortcut.as_str().to_owned(),
                        first_action,
                        second_action: definition.name,
                    });
                }
            }
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn overrides(entries: &[(&str, &str)]) -> BTreeMap<String, Vec<String>> {
        entries
            .iter()
            .map(|(action, shortcut)| ((*action).to_owned(), vec![(*shortcut).to_owned()]))
            .collect()
    }

    fn override_lists(entries: &[(&str, &[&str])]) -> BTreeMap<String, Vec<String>> {
        entries
            .iter()
            .map(|(action, shortcuts)| {
                (
                    (*action).to_owned(),
                    shortcuts
                        .iter()
                        .map(|shortcut| (*shortcut).to_owned())
                        .collect(),
                )
            })
            .collect()
    }

    #[test]
    fn inventory_exactly_matches_tui_semantic_actions_and_defaults() {
        let expected = [
            ("submit", &["enter"][..]),
            ("follow_up", &["alt+enter"][..]),
            ("newline", &["ctrl+j", "alt+enter"][..]),
            ("paste", &["ctrl+v"][..]),
            ("abort", &["ctrl+c", "esc"][..]),
            ("quit", &["ctrl+c", "ctrl+d"][..]),
            ("toggle_mode", &["shift+tab"][..]),
            ("thinking", &["ctrl+t"][..]),
            ("models", &["alt+m"][..]),
            ("agents", &["alt+a"][..]),
            ("processes", &["alt+p"][..]),
            ("page_up", &["pgup"][..]),
            ("page_down", &["pgdown"][..]),
            ("top", &["home"][..]),
            ("bottom", &["end"][..]),
            ("line_up", &["ctrl+up"][..]),
            ("line_down", &["ctrl+down"][..]),
            ("picker_up", &["up", "left", "k"][..]),
            ("picker_down", &["down", "right", "j"][..]),
            ("picker_previous", &["shift+tab"][..]),
            ("picker_next", &["tab"][..]),
            ("picker_page_up", &["pgup"][..]),
            ("picker_page_down", &["pgdown"][..]),
            ("picker_top", &["home"][..]),
            ("picker_bottom", &["end"][..]),
            ("accept", &["enter"][..]),
            ("close", &["esc"][..]),
            ("branch_fork", &["f"][..]),
            ("branch_rename", &["r"][..]),
            ("branch_delete", &["d"][..]),
            ("confirm", &["y"][..]),
        ];
        let actual: Vec<_> = ACTION_INVENTORY
            .iter()
            .map(|definition| (definition.name, definition.defaults))
            .collect();
        assert_eq!(actual, expected);
        assert_eq!(StartupBindingConfiguration::default().catalog().len(), 31);
    }

    #[test]
    fn settings_context_exposes_only_settings_safe_shortcuts() {
        assert_eq!(BindingContext::Settings.stable_name(), "settings");
        assert_eq!(BindingContext::Settings.gpui_selector(), "DesktopSettings");
        assert!(
            action_definition("models")
                .unwrap()
                .contexts
                .contains(&BindingContext::Settings)
        );
        assert!(
            action_definition("thinking")
                .unwrap()
                .contexts
                .contains(&BindingContext::Settings)
        );
        assert!(
            !action_definition("agents")
                .unwrap()
                .contexts
                .contains(&BindingContext::Settings)
        );
        assert!(
            !action_definition("processes")
                .unwrap()
                .contexts
                .contains(&BindingContext::Settings)
        );
    }

    #[test]
    fn canonical_shortcuts_normalize_persisted_form() {
        for (input, expected) in [
            (" CTRL+UP ", "ctrl+up"),
            ("Escape", "esc"),
            ("ALT+ENTER", "alt+enter"),
            ("SHIFT+TAB", "shift+tab"),
            ("K", "k"),
            ("é", "é"),
        ] {
            assert_eq!(CanonicalShortcut::parse(input).unwrap().as_str(), expected);
        }
    }

    #[test]
    fn invalid_shortcuts_are_rejected() {
        for input in [
            "",
            " ",
            "ctrl-up",
            "ctrl + up",
            "cmd+x",
            "shift+x",
            "ctrl+left",
            "ctrl+alt+x",
            "ctrl++",
            "+",
            "-",
            "two",
            "ctrl+two",
            "\n",
        ] {
            assert!(
                CanonicalShortcut::parse(input).is_err(),
                "unexpectedly accepted {input:?}"
            );
        }
        for input in [
            "enter",
            "esc",
            "tab",
            "up",
            "down",
            "left",
            "right",
            "home",
            "end",
            "pgup",
            "pgdown",
            "backspace",
            "delete",
            "x",
            "ctrl+x",
            "alt+x",
            "ctrl+enter",
            "ctrl+up",
            "ctrl+down",
            "alt+enter",
            "alt+up",
            "alt+down",
            "shift+tab",
        ] {
            assert!(
                CanonicalShortcut::parse(input).is_ok(),
                "unexpectedly rejected {input:?}"
            );
        }
    }

    #[test]
    fn gpui_translation_is_explicit_and_does_not_leak_into_persistence() {
        for (canonical, gpui) in [
            ("ctrl+up", "ctrl-up"),
            ("ctrl+c", "ctrl-c"),
            ("alt+enter", "alt-enter"),
            ("shift+tab", "shift-tab"),
            ("esc", "escape"),
            ("pgup", "pageup"),
            ("pgdown", "pagedown"),
            ("enter", "enter"),
        ] {
            let shortcut = CanonicalShortcut::parse(canonical).unwrap();
            assert_eq!(shortcut.as_str(), canonical);
            assert_eq!(shortcut.gpui_keystroke(), gpui);
        }
    }

    #[test]
    fn override_merge_is_deterministic_and_retains_emergency_keys() {
        let plan = StartupBindingConfiguration::from_overrides(&override_lists(&[
            ("quit", &["ctrl+q"]),
            ("close", &["q"]),
            ("abort", &["ctrl+x"]),
            ("models", &[" ALT+Z ", "ctrl+m", "alt+z"]),
        ]))
        .unwrap();
        assert_eq!(
            plan.effective_shortcuts("abort").unwrap(),
            ["ctrl+x", "ctrl+c", "esc"]
        );
        assert_eq!(
            plan.effective_shortcuts("quit").unwrap(),
            ["ctrl+q", "ctrl+c"]
        );
        assert_eq!(plan.effective_shortcuts("close").unwrap(), ["q", "esc"]);
        assert_eq!(
            plan.effective_shortcuts("models").unwrap(),
            ["alt+z", "ctrl+m"]
        );
        assert_eq!(plan.effective_shortcuts("submit").unwrap(), ["enter"]);
        assert_eq!(plan.normalized_overrides()["models"], ["alt+z", "ctrl+m"]);
    }

    #[test]
    fn empty_override_unbinds_except_for_emergency_retention() {
        let plan = StartupBindingConfiguration::from_overrides(&override_lists(&[
            ("submit", &[]),
            ("models", &[]),
            ("abort", &[]),
            ("quit", &[]),
            ("close", &[]),
        ]))
        .unwrap();
        assert!(plan.effective_shortcuts("submit").unwrap().is_empty());
        assert!(plan.effective_shortcuts("models").unwrap().is_empty());
        assert_eq!(
            plan.effective_shortcuts("abort").unwrap(),
            ["ctrl+c", "esc"]
        );
        assert_eq!(plan.effective_shortcuts("quit").unwrap(), ["ctrl+c"]);
        assert_eq!(plan.effective_shortcuts("close").unwrap(), ["esc"]);
        assert!(plan.normalized_overrides()["submit"].is_empty());
        assert!(
            plan.startup_specs()
                .iter()
                .all(|spec| spec.action != "submit")
        );
        assert!(plan.help_text().contains(
            "  Submit: unbound
"
        ));
    }

    #[test]
    fn unknown_action_and_invalid_override_report_the_action() {
        let error =
            StartupBindingConfiguration::from_overrides(&overrides(&[("wat", "x")])).unwrap_err();
        assert_eq!(
            error,
            BindingConfigurationError::UnknownAction("wat".into())
        );

        let error =
            StartupBindingConfiguration::from_overrides(&overrides(&[("submit", "ctrl-up")]))
                .unwrap_err();
        assert!(matches!(
            error,
            BindingConfigurationError::InvalidShortcut { ref action, .. } if action == "submit"
        ));
    }

    #[test]
    fn collisions_are_checked_only_in_overlapping_states() {
        let error =
            StartupBindingConfiguration::from_overrides(&overrides(&[("submit", "ctrl+t")]))
                .unwrap_err();
        assert!(matches!(
            error,
            BindingConfigurationError::Collision {
                context: BindingContext::ComposerIdle,
                shortcut,
                first_action: "submit",
                second_action: "thinking",
            } if shortcut == "ctrl+t"
        ));

        let error = StartupBindingConfiguration::from_overrides(&override_lists(&[(
            "submit",
            &["ctrl+s", "ctrl+t"],
        )]))
        .unwrap_err();
        assert!(matches!(
            error,
            BindingConfigurationError::Collision {
                context: BindingContext::ComposerIdle,
                shortcut,
                first_action: "submit",
                second_action: "thinking",
            } if shortcut == "ctrl+t"
        ));

        let error =
            StartupBindingConfiguration::from_overrides(&overrides(&[("follow_up", "enter")]))
                .unwrap_err();
        assert!(matches!(
            error,
            BindingConfigurationError::Collision {
                context: BindingContext::ComposerBusy,
                shortcut,
                first_action: "submit",
                second_action: "follow_up",
            } if shortcut == "enter"
        ));

        // Transcript and picker paging deliberately reuse the same default
        // keys in mutually exclusive contexts.
        let plan = StartupBindingConfiguration::default();
        assert_eq!(plan.effective_shortcuts("page_up").unwrap(), ["pgup"]);
        assert_eq!(
            plan.effective_shortcuts("picker_page_up").unwrap(),
            ["pgup"]
        );
    }

    #[test]
    fn emergency_keys_cannot_be_shadowed() {
        for (action, key) in [
            ("submit", "ctrl+c"),
            ("submit", "esc"),
            ("follow_up", "ctrl+c"),
            ("follow_up", "esc"),
            ("accept", "esc"),
            ("branch_fork", "esc"),
        ] {
            let error = StartupBindingConfiguration::from_overrides(&overrides(&[(action, key)]))
                .unwrap_err();
            assert!(
                matches!(error, BindingConfigurationError::Collision { .. }),
                "{action}={key} should collide with a retained emergency key"
            );
        }
    }

    #[test]
    fn startup_specs_cover_every_effective_context_and_translate_keys() {
        let plan = StartupBindingConfiguration::default();
        let specs = plan.startup_specs();
        let expected_count: usize = ACTION_INVENTORY
            .iter()
            .map(|definition| {
                definition.contexts.len() * plan.effective_shortcuts(definition.name).unwrap().len()
            })
            .sum();
        assert_eq!(specs.len(), expected_count);
        assert_eq!(
            specs.iter().find(|spec| spec.action == "line_up").unwrap(),
            &StartupBindingSpec {
                action: "line_up",
                context: BindingContext::Transcript,
                gpui_context: "DesktopTranscript",
                canonical_shortcut: "ctrl+up".into(),
                gpui_keystroke: "ctrl-up".into(),
            }
        );
        assert!(specs.iter().any(|spec| {
            spec.action == "close"
                && spec.canonical_shortcut == "esc"
                && spec.gpui_keystroke == "escape"
        }));
    }

    #[test]
    fn catalog_and_help_are_stable_across_override_insertion_order() {
        let first = StartupBindingConfiguration::from_overrides(&overrides(&[
            ("models", "alt+z"),
            ("submit", "ctrl+s"),
        ]))
        .unwrap();
        let second = StartupBindingConfiguration::from_overrides(&overrides(&[
            ("submit", "ctrl+s"),
            ("models", "alt+z"),
        ]))
        .unwrap();
        assert_eq!(first.catalog(), second.catalog());
        assert_eq!(first.startup_specs(), second.startup_specs());
        assert_eq!(first.help_text(), second.help_text());

        let help = first.help_text();
        assert!(help.starts_with("Keyboard shortcuts\n\nComposer\n"));
        assert!(help.contains("  Submit: ctrl+s\n"));
        assert!(help.contains("\nGlobal\n  Models: alt+z\n"));
        assert!(help.contains("\nTranscript\n"));
        assert!(help.contains("\nPickers\n"));
        assert!(help.contains("\nBranches\n"));
        assert!(help.ends_with("Shortcut changes can be applied without restarting.\n"));
    }

    #[test]
    fn effective_changes_produce_ordered_runtime_replacement_plans() {
        let current = StartupBindingConfiguration::default();
        let changed = plan_change(&current, &overrides(&[("submit", "ctrl+s")])).unwrap();
        assert_eq!(changed.effect, BindingChangeEffect::RuntimeApplicable);
        assert!(changed.can_apply_runtime());
        assert_eq!(
            changed.next.effective_shortcuts("submit").unwrap(),
            ["ctrl+s"]
        );
        assert_eq!(
            changed.runtime_plan.no_action_specs,
            current
                .startup_specs()
                .into_iter()
                .map(|spec| NoActionBindingSpec {
                    previous_action: spec.action,
                    context: spec.context,
                    gpui_context: spec.gpui_context,
                    canonical_shortcut: spec.canonical_shortcut,
                    gpui_keystroke: spec.gpui_keystroke,
                })
                .collect::<Vec<_>>()
        );
        assert_eq!(
            changed.runtime_plan.action_specs,
            changed.next.startup_specs()
        );

        let unchanged = plan_change(&current, &BTreeMap::new()).unwrap();
        assert_eq!(unchanged.effect, BindingChangeEffect::Unchanged);
        assert!(!unchanged.can_apply_runtime());
        assert!(unchanged.runtime_plan.is_empty());

        let normalized =
            StartupBindingConfiguration::from_overrides(&overrides(&[("line_up", " CTRL+UP ")]))
                .unwrap();
        let unchanged = plan_change(&normalized, &overrides(&[("line_up", "ctrl+up")])).unwrap();
        assert_eq!(unchanged.effect, BindingChangeEffect::Unchanged);

        // Removing a redundant override changes persistence metadata, not the
        // effective GPUI bindings, so it needs no replacement plan.
        let unchanged = plan_change(&normalized, &BTreeMap::new()).unwrap();
        assert_eq!(unchanged.effect, BindingChangeEffect::Unchanged);
        assert!(unchanged.runtime_plan.is_empty());
    }
}
