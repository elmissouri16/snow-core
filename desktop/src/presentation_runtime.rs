//! Runtime application of Snow-owned themes and semantic keybindings.
//!
//! Snow RPC remains the persistence authority. This module retains only the
//! bounded, decoded projections needed by GPUI and can replace an installed
//! keymap without restarting the desktop process.

use std::{
    collections::{BTreeMap, BTreeSet},
    error::Error,
    fmt,
};

use gpui::{App, Global, KeyBinding, NoAction, actions, rgb};
use gpui_component::theme::Theme;

use crate::{
    keybindings::{
        ACTION_INVENTORY, BindingConfigurationError, RuntimeBindingPlan,
        StartupBindingConfiguration, StartupBindingSpec, plan_change,
    },
    snow::{Keybindings, ThemeCatalog},
    theme_palette::{NativeAppearance, Rgb, SemanticPalette, ThemePaletteError, ThemePaletteState},
};

actions!(
    snow_semantic_keys,
    [
        SubmitAction,
        FollowUpAction,
        NewlineAction,
        PasteAction,
        AbortAction,
        QuitAction,
        ToggleModeAction,
        ThinkingAction,
        ModelsAction,
        AgentsAction,
        ProcessesAction,
        PageUpAction,
        PageDownAction,
        TopAction,
        BottomAction,
        LineUpAction,
        LineDownAction,
        PickerUpAction,
        PickerDownAction,
        PickerPreviousAction,
        PickerNextAction,
        PickerPageUpAction,
        PickerPageDownAction,
        PickerTopAction,
        PickerBottomAction,
        AcceptAction,
        CloseAction,
        BranchForkAction,
        BranchRenameAction,
        BranchDeleteAction,
        ConfirmAction
    ]
);

#[derive(Debug)]
pub(crate) struct PresentationRuntimeState {
    palette: Option<ThemePaletteState>,
    bindings: StartupBindingConfiguration,
}

impl Global for PresentationRuntimeState {}

impl PresentationRuntimeState {
    pub(crate) fn palette(&self) -> Option<&ThemePaletteState> {
        self.palette.as_ref()
    }

    pub(crate) fn bindings(&self) -> &StartupBindingConfiguration {
        &self.bindings
    }

    pub(crate) fn help_text(&self) -> String {
        self.bindings.help_text()
    }
}

#[derive(Debug)]
pub(crate) enum PresentationRuntimeError {
    ThemeCatalog(ThemePaletteError),
    ThemeEncoding,
    Keybindings(BindingConfigurationError),
    DuplicateAction,
    MissingAction,
}

impl fmt::Display for PresentationRuntimeError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::ThemeCatalog(error) => write!(formatter, "invalid theme catalog: {error}"),
            Self::ThemeEncoding => formatter.write_str("theme catalog could not be projected"),
            Self::Keybindings(error) => write!(formatter, "invalid keybindings: {error}"),
            Self::DuplicateAction => formatter.write_str("keybindings contain a duplicate action"),
            Self::MissingAction => formatter.write_str("keybindings omit a semantic action"),
        }
    }
}

impl Error for PresentationRuntimeError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        match self {
            Self::ThemeCatalog(error) => Some(error),
            Self::Keybindings(error) => Some(error),
            _ => None,
        }
    }
}

impl From<ThemePaletteError> for PresentationRuntimeError {
    fn from(error: ThemePaletteError) -> Self {
        Self::ThemeCatalog(error)
    }
}

impl From<BindingConfigurationError> for PresentationRuntimeError {
    fn from(error: BindingConfigurationError) -> Self {
        Self::Keybindings(error)
    }
}

/// Install globals and the built-in semantic keymap.
pub(crate) fn init(cx: &mut App) {
    let bindings = StartupBindingConfiguration::default();
    install_action_specs(&bindings.startup_specs(), cx);
    cx.set_global(PresentationRuntimeState {
        palette: None,
        bindings,
    });
}

/// Replace the bounded palette catalog and apply its canonical selection.
pub(crate) fn apply_theme_catalog(
    catalog: ThemeCatalog,
    cx: &mut App,
) -> Result<(), PresentationRuntimeError> {
    let json =
        serde_json::to_string(&catalog).map_err(|_| PresentationRuntimeError::ThemeEncoding)?;
    let palette = ThemePaletteState::decode_json(&json)?;
    cx.global_mut::<PresentationRuntimeState>().palette = Some(palette);
    reapply_palette(cx);
    Ok(())
}

/// Select an already-catalogued Snow theme. Call only after Snow confirms the
/// settings update, so the RPC/config value remains authoritative.
pub(crate) fn select_theme(selected: &str, cx: &mut App) -> Result<(), PresentationRuntimeError> {
    let Some(palette) = cx.global_mut::<PresentationRuntimeState>().palette.as_mut() else {
        return Ok(());
    };
    palette.select(selected)?;
    reapply_palette(cx);
    Ok(())
}

/// Re-resolve adaptive colors after native System/Light/Dark changes.
pub(crate) fn reapply_palette(cx: &mut App) {
    let Some(state) = cx.try_global::<PresentationRuntimeState>() else {
        return;
    };
    let appearance = if Theme::global(cx).is_dark() {
        NativeAppearance::Dark
    } else {
        NativeAppearance::Light
    };
    let colors = state
        .palette
        .as_ref()
        .map(|palette| palette.active_palette(appearance).colors);
    let Some(colors) = colors else {
        return;
    };
    apply_semantic_palette(colors, cx);
}

fn apply_semantic_palette(colors: SemanticPalette, cx: &mut App) {
    let theme = Theme::global_mut(cx);
    let accent = to_hsla(colors.accent);
    theme.primary = accent;
    theme.info = accent;
    theme.muted_foreground = to_hsla(colors.muted);
    theme.foreground = to_hsla(colors.foreground);
    theme.warning = to_hsla(colors.warning);
    theme.danger = to_hsla(colors.error);
    theme.success = to_hsla(colors.success);
    theme.border = to_hsla(colors.separator);
    // gpui-component 0.5 has no Theme::sync_base API. Its pinned equivalent
    // is to mutate the one global Theme snapshot and refresh every window.
    cx.refresh_windows();
}

fn to_hsla(color: Rgb) -> gpui::Hsla {
    let value =
        (u32::from(color.red) << 16) | (u32::from(color.green) << 8) | u32::from(color.blue);
    rgb(value).into()
}

/// Validate and install the complete effective inventory returned by Snow.
pub(crate) fn apply_keybindings(
    keybindings: Keybindings,
    cx: &mut App,
) -> Result<(), PresentationRuntimeError> {
    let overrides = overrides_from_rpc(&keybindings)?;
    let current = cx.global::<PresentationRuntimeState>().bindings.clone();
    let change = plan_change(&current, &overrides)?;
    if change.can_apply_runtime() {
        install_runtime_plan(&change.runtime_plan, cx);
    }
    cx.global_mut::<PresentationRuntimeState>().bindings = change.next;
    Ok(())
}

fn overrides_from_rpc(
    keybindings: &Keybindings,
) -> Result<BTreeMap<String, Vec<String>>, PresentationRuntimeError> {
    let expected: BTreeSet<&str> = ACTION_INVENTORY.iter().map(|action| action.name).collect();
    let mut seen = BTreeSet::new();
    let mut overrides = BTreeMap::new();
    for action in &keybindings.actions {
        if !seen.insert(action.name.as_str()) {
            return Err(PresentationRuntimeError::DuplicateAction);
        }
        overrides.insert(action.name.clone(), action.effective.clone());
    }
    if seen != expected {
        return Err(PresentationRuntimeError::MissingAction);
    }
    Ok(overrides)
}

fn install_runtime_plan(plan: &RuntimeBindingPlan, cx: &mut App) {
    cx.bind_keys(plan.no_action_specs.iter().map(|spec| {
        KeyBinding::new(
            spec.gpui_keystroke.as_str(),
            NoAction,
            Some(spec.gpui_context),
        )
    }));
    install_action_specs(&plan.action_specs, cx);
}

fn install_action_specs(specs: &[StartupBindingSpec], cx: &mut App) {
    cx.bind_keys(specs.iter().map(key_binding_for_spec));
}

fn key_binding_for_spec(spec: &StartupBindingSpec) -> KeyBinding {
    macro_rules! binding {
        ($action:expr) => {
            KeyBinding::new(
                spec.gpui_keystroke.as_str(),
                $action,
                Some(spec.gpui_context),
            )
        };
    }
    match spec.action {
        "submit" => binding!(SubmitAction),
        "follow_up" => binding!(FollowUpAction),
        "newline" => binding!(NewlineAction),
        "paste" => binding!(PasteAction),
        "abort" => binding!(AbortAction),
        "quit" => binding!(QuitAction),
        "toggle_mode" => binding!(ToggleModeAction),
        "thinking" => binding!(ThinkingAction),
        "models" => binding!(ModelsAction),
        "agents" => binding!(AgentsAction),
        "processes" => binding!(ProcessesAction),
        "page_up" => binding!(PageUpAction),
        "page_down" => binding!(PageDownAction),
        "top" => binding!(TopAction),
        "bottom" => binding!(BottomAction),
        "line_up" => binding!(LineUpAction),
        "line_down" => binding!(LineDownAction),
        "picker_up" => binding!(PickerUpAction),
        "picker_down" => binding!(PickerDownAction),
        "picker_previous" => binding!(PickerPreviousAction),
        "picker_next" => binding!(PickerNextAction),
        "picker_page_up" => binding!(PickerPageUpAction),
        "picker_page_down" => binding!(PickerPageDownAction),
        "picker_top" => binding!(PickerTopAction),
        "picker_bottom" => binding!(PickerBottomAction),
        "accept" => binding!(AcceptAction),
        "close" => binding!(CloseAction),
        "branch_fork" => binding!(BranchForkAction),
        "branch_rename" => binding!(BranchRenameAction),
        "branch_delete" => binding!(BranchDeleteAction),
        "confirm" => binding!(ConfirmAction),
        action => panic!("validated semantic action has no GPUI mapping: {action}"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::snow::KeybindingAction;

    #[test]
    fn every_inventory_action_has_a_runtime_mapping() {
        let configuration = StartupBindingConfiguration::default();
        for spec in configuration.startup_specs() {
            let _ = key_binding_for_spec(&spec);
        }
        assert_eq!(ACTION_INVENTORY.len(), 31);
    }

    #[test]
    fn rpc_inventory_must_be_complete_and_unique() {
        let complete = Keybindings {
            project_allowed: true,
            actions: ACTION_INVENTORY
                .iter()
                .map(|definition| KeybindingAction {
                    name: definition.name.to_owned(),
                    effective: definition
                        .defaults
                        .iter()
                        .map(|value| (*value).to_owned())
                        .collect(),
                    ..KeybindingAction::default()
                })
                .collect(),
        };
        assert_eq!(overrides_from_rpc(&complete).unwrap().len(), 31);

        let mut missing = complete.clone();
        missing.actions.pop();
        assert!(matches!(
            overrides_from_rpc(&missing),
            Err(PresentationRuntimeError::MissingAction)
        ));

        let mut duplicate = complete;
        duplicate.actions.push(duplicate.actions[0].clone());
        assert!(matches!(
            overrides_from_rpc(&duplicate),
            Err(PresentationRuntimeError::DuplicateAction)
        ));
    }

    #[test]
    fn runtime_plan_disables_old_bindings_before_installing_new() {
        let current = StartupBindingConfiguration::default();
        let mut overrides = BTreeMap::new();
        overrides.insert("models".to_owned(), vec!["ctrl+m".to_owned()]);
        let change = plan_change(&current, &overrides).unwrap();
        assert!(change.can_apply_runtime());
        assert!(!change.runtime_plan.no_action_specs.is_empty());
        assert!(!change.runtime_plan.action_specs.is_empty());
        assert!(change.next.help_text().contains("Models"));
    }

    #[test]
    fn semantic_palette_mapping_keeps_all_seven_roles_distinct() {
        let colors = SemanticPalette {
            accent: Rgb::new(1, 2, 3),
            muted: Rgb::new(4, 5, 6),
            foreground: Rgb::new(7, 8, 9),
            warning: Rgb::new(10, 11, 12),
            error: Rgb::new(13, 14, 15),
            success: Rgb::new(16, 17, 18),
            separator: Rgb::new(19, 20, 21),
        };
        assert_ne!(to_hsla(colors.accent), to_hsla(colors.muted));
        assert_ne!(to_hsla(colors.foreground), to_hsla(colors.warning));
        assert_ne!(to_hsla(colors.error), to_hsla(colors.success));
        assert_ne!(to_hsla(colors.separator), to_hsla(colors.accent));
    }
}
