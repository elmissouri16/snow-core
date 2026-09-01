//! Desktop appearance preference and GPUI theme synchronization.
//!
//! The persisted value is owned by a GPUI global so startup, settings UI, and
//! window-appearance observers all consult one source of truth. Persistence is
//! committed before an in-memory/theme change: if the atomic preference write
//! fails, both the selected appearance and active theme remain unchanged.

use std::fmt;

use gpui::{App, BorrowAppContext, Global, Window};
use gpui_component::theme::{Theme, ThemeMode};

use crate::{
    preferences::{Appearance, DesktopPreferences, PreferencesError, load, save},
    presentation_runtime,
};

/// Application-global desktop presentation preferences.
#[derive(Debug)]
pub struct AppearanceState {
    preferences: DesktopPreferences,
    load_diagnostic: Option<String>,
    last_error: Option<String>,
}

impl Global for AppearanceState {}

const MAX_DIAGNOSTIC_CHARS: usize = 1_024;

impl AppearanceState {
    /// Return all currently committed desktop preferences.
    pub fn preferences(&self) -> &DesktopPreferences {
        &self.preferences
    }

    /// Return the currently committed appearance preference.
    pub fn appearance(&self) -> Appearance {
        self.preferences.appearance
    }

    /// Return the non-fatal startup load diagnostic, if defaults were used.
    pub fn load_diagnostic(&self) -> Option<&str> {
        self.load_diagnostic.as_deref()
    }

    /// Return the latest runtime persistence error, if any.
    pub fn last_error(&self) -> Option<&str> {
        self.last_error.as_deref()
    }
}

/// Load desktop preferences, install their GPUI global, and apply the selected
/// theme. Call this after `gpui_component::init` has installed its theme global.
///
/// Invalid or unreadable preferences do not prevent desktop startup. Version-one
/// defaults are installed instead, and the bounded error text remains available
/// through [`AppearanceState::load_diagnostic`] for presentation to the user.
pub fn init(cx: &mut App) {
    let (preferences, load_diagnostic) = load_or_default(load);
    let appearance = preferences.appearance;
    cx.set_global(AppearanceState {
        preferences,
        load_diagnostic,
        last_error: None,
    });
    apply_appearance(appearance, None, cx);
}

/// Reapply the committed appearance to a window. This is useful immediately
/// after opening a window because system appearance is more reliable from the
/// concrete window than from application-wide startup state on Linux.
pub fn apply_current(window: Option<&mut Window>, cx: &mut App) {
    let appearance = cx.global::<AppearanceState>().appearance();
    apply_appearance(appearance, window, cx);
    presentation_runtime::reapply_palette(cx);
}

/// Persist and apply a new appearance preference.
///
/// The atomic preferences write completes before the global or theme changes.
/// On failure, the previous global/theme remain authoritative and the same
/// error is both returned and exposed through [`AppearanceState::last_error`].
pub fn set_appearance(
    appearance: Appearance,
    window: Option<&mut Window>,
    cx: &mut App,
) -> Result<(), PreferencesError> {
    let result = cx
        .update_global::<AppearanceState, _>(|state, _| commit_appearance(state, appearance, save));
    if result.is_ok() {
        apply_appearance(appearance, window, cx);
        presentation_runtime::reapply_palette(cx);
    }
    result
}

/// Observer callback helper. Explicit light/dark choices deliberately ignore
/// operating-system appearance notifications.
pub fn sync_system_appearance_if_selected(window: &mut Window, cx: &mut App) {
    if cx
        .try_global::<AppearanceState>()
        .is_some_and(|state| state.appearance() == Appearance::System)
    {
        Theme::sync_system_appearance(Some(window), cx);
        presentation_runtime::reapply_palette(cx);
    }
}

fn explicit_theme_mode(appearance: Appearance) -> Option<ThemeMode> {
    match appearance {
        Appearance::System => None,
        Appearance::Light => Some(ThemeMode::Light),
        Appearance::Dark => Some(ThemeMode::Dark),
    }
}

fn apply_appearance(appearance: Appearance, window: Option<&mut Window>, cx: &mut App) {
    let refresh_all = window.is_none();
    match explicit_theme_mode(appearance) {
        Some(mode) => Theme::change(mode, window, cx),
        None => Theme::sync_system_appearance(window, cx),
    }
    if refresh_all {
        cx.refresh_windows();
    }
}

fn load_or_default<E>(
    loader: impl FnOnce() -> Result<DesktopPreferences, E>,
) -> (DesktopPreferences, Option<String>)
where
    E: fmt::Display,
{
    match loader() {
        Ok(preferences) => (preferences, None),
        Err(error) => (
            DesktopPreferences::default(),
            Some(bounded_diagnostic(error)),
        ),
    }
}

fn bounded_diagnostic(error: impl fmt::Display) -> String {
    let text = error.to_string();
    let mut chars = text.chars();
    let prefix: String = chars.by_ref().take(MAX_DIAGNOSTIC_CHARS).collect();
    if chars.next().is_some() {
        format!("{prefix}…")
    } else {
        prefix
    }
}

fn commit_appearance<E>(
    state: &mut AppearanceState,
    appearance: Appearance,
    persist: impl FnOnce(&DesktopPreferences) -> Result<(), E>,
) -> Result<(), E>
where
    E: fmt::Display,
{
    let mut next = state.preferences.clone();
    next.appearance = appearance;
    match persist(&next) {
        Ok(()) => {
            state.preferences = next;
            state.load_diagnostic = None;
            state.last_error = None;
            Ok(())
        }
        Err(error) => {
            state.last_error = Some(bounded_diagnostic(&error));
            Err(error)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn state(appearance: Appearance) -> AppearanceState {
        let mut preferences = DesktopPreferences::default();
        preferences.appearance = appearance;
        AppearanceState {
            preferences,
            load_diagnostic: None,
            last_error: None,
        }
    }

    #[test]
    fn explicit_modes_do_not_follow_system_appearance() {
        assert_eq!(explicit_theme_mode(Appearance::System), None);
        assert_eq!(
            explicit_theme_mode(Appearance::Light),
            Some(ThemeMode::Light)
        );
        assert_eq!(explicit_theme_mode(Appearance::Dark), Some(ThemeMode::Dark));
    }

    #[test]
    fn load_failure_uses_defaults_and_retains_diagnostic() {
        let (preferences, diagnostic) =
            load_or_default(|| -> Result<DesktopPreferences, &'static str> { Err("bad file") });
        assert_eq!(preferences, DesktopPreferences::default());
        assert_eq!(diagnostic.as_deref(), Some("bad file"));
    }

    #[test]
    fn successful_persistence_commits_only_the_proposed_preferences() {
        let mut state = state(Appearance::System);
        state.load_diagnostic = Some("old load warning".into());
        state.last_error = Some("old error".into());
        let mut persisted = None;

        commit_appearance(&mut state, Appearance::Dark, |next| {
            persisted = Some(next.clone());
            Ok::<_, &'static str>(())
        })
        .unwrap();

        assert_eq!(state.appearance(), Appearance::Dark);
        assert_eq!(persisted.unwrap().appearance, Appearance::Dark);
        assert_eq!(state.load_diagnostic(), None);
        assert_eq!(state.last_error(), None);
    }

    #[test]
    fn diagnostics_are_character_bounded() {
        let diagnostic = bounded_diagnostic("x".repeat(MAX_DIAGNOSTIC_CHARS + 10));
        assert_eq!(diagnostic.chars().count(), MAX_DIAGNOSTIC_CHARS + 1);
        assert!(diagnostic.ends_with('…'));
    }

    #[test]
    fn failed_persistence_rolls_back_selection_and_exposes_error() {
        let mut state = state(Appearance::Light);
        let original = state.preferences.clone();

        let error = commit_appearance(&mut state, Appearance::Dark, |_| Err::<(), _>("disk full"))
            .unwrap_err();

        assert_eq!(error, "disk full");
        assert_eq!(state.preferences(), &original);
        assert_eq!(state.appearance(), Appearance::Light);
        assert_eq!(state.last_error(), Some("disk full"));
    }
}
