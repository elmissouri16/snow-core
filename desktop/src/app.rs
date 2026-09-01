use gpui::{App, Application, Bounds, WindowBounds, WindowOptions, prelude::*, px, size};
use gpui_component::Root;

use crate::{
    appearance, presentation_runtime,
    workspace::{self, Workspace},
};

pub fn run() {
    Application::new()
        .with_assets(gpui_component_assets::Assets)
        .run(|cx: &mut App| {
            gpui_component::init(cx);
            appearance::init(cx);
            presentation_runtime::init(cx);
            workspace::init(cx);
            cx.on_window_closed(move |cx| {
                if cx.windows().is_empty() {
                    cx.quit();
                }
            })
            .detach();

            let bounds = Bounds::centered(None, size(px(1280.), px(820.)), cx);
            cx.open_window(
                WindowOptions {
                    window_bounds: Some(WindowBounds::Windowed(bounds)),
                    window_min_size: Some(size(px(900.), px(600.))),
                    titlebar: Some(gpui::TitlebarOptions {
                        title: Some("Snow Desktop".into()),
                        ..Default::default()
                    }),
                    ..Default::default()
                },
                move |window, cx| {
                    // Re-resolve System against the actual window. This is more
                    // reliable than the application-wide startup appearance on
                    // Linux, and explicit Light/Dark choices remain authoritative.
                    appearance::apply_current(Some(window), cx);
                    window
                        .observe_window_appearance(|window, cx| {
                            appearance::sync_system_appearance_if_selected(window, cx);
                        })
                        .detach();
                    let workspace = cx.new(|cx| Workspace::new(window, cx));
                    cx.new(|cx| Root::new(workspace, window, cx))
                },
            )
            .expect("failed to open Snow Desktop window");

            cx.activate(true);
        });
}

#[cfg(test)]
mod tests {
    use gpui::AssetSource;

    #[test]
    fn bundled_assets_include_copy_icon() {
        let result = gpui_component_assets::Assets.load("icons/copy.svg");
        assert!(matches!(result, Ok(Some(bytes)) if !bytes.is_empty()));
    }
}
