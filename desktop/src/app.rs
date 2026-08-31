use gpui::{App, Application, Bounds, WindowBounds, WindowOptions, prelude::*, px, size};
use gpui_component::Root;

use crate::workspace::{self, Workspace};

pub fn run() {
    Application::new().run(|cx: &mut App| {
        gpui_component::init(cx);
        workspace::init(cx);
        cx.on_window_closed(move |cx| {
            if cx.windows().is_empty() {
                cx.quit();
            }
        })
        .detach();

        let bounds = Bounds::centered(None, size(px(1080.), px(760.)), cx);
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                window_min_size: Some(size(px(800.), px(560.))),
                titlebar: Some(gpui::TitlebarOptions {
                    title: Some("Snow Desktop".into()),
                    ..Default::default()
                }),
                ..Default::default()
            },
            move |window, cx| {
                let workspace = cx.new(|cx| Workspace::new(window, cx));
                cx.new(|cx| Root::new(workspace, window, cx))
            },
        )
        .expect("failed to open Snow Desktop window");

        cx.activate(true);
    });
}
