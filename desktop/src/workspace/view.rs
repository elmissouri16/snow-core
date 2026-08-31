use super::*;

const CONVERSATION_WIDTH: f32 = 760.;
const PICKER_MAX_HEIGHT: f32 = 260.;

impl Workspace {
    pub(super) fn render_workspace(
        &self,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        v_flex()
            .size_full()
            .min_w(px(800.))
            .min_h(px(560.))
            .overflow_hidden()
            .bg(cx.theme().background)
            .text_color(cx.theme().foreground)
            .child(self.render_top_bar(cx))
            .when(self.session_menu_open, |workspace| {
                workspace.child(self.render_session_menu(cx))
            })
            .child(self.render_transcript(window, cx))
            .child(self.render_composer(cx))
    }

    fn render_top_bar(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme();
        let (connection, connection_color) = self.connection_label(theme);
        let session_name = if self.state.session_name.is_empty() {
            "Session"
        } else {
            &self.state.session_name
        };
        let active_branch = self
            .state
            .branches
            .iter()
            .find(|branch| branch.active)
            .map(|branch| {
                if branch.name.is_empty() {
                    branch.id.as_str()
                } else {
                    branch.name.as_str()
                }
            });
        let session_branch_label = active_branch
            .map(|branch| format!("{session_name} / {branch}  ▾"))
            .unwrap_or_else(|| format!("{session_name}  ▾"));

        h_flex()
            .h(px(50.))
            .w_full()
            .flex_shrink_0()
            .px_5()
            .items_center()
            .justify_between()
            .border_b_1()
            .border_color(theme.border)
            .child(
                h_flex()
                    .min_w(px(0.))
                    .items_center()
                    .gap_2()
                    .child(div().text_sm().font_semibold().child("Snow"))
                    .child(
                        div()
                            .text_xs()
                            .text_color(theme.muted_foreground)
                            .child("/"),
                    )
                    .child(
                        div()
                            .min_w(px(0.))
                            .max_w(px(280.))
                            .overflow_hidden()
                            .text_ellipsis()
                            .text_xs()
                            .text_color(theme.muted_foreground)
                            .child(self.project_label()),
                    )
                    .child(
                        div()
                            .text_xs()
                            .text_color(theme.muted_foreground)
                            .child("/"),
                    )
                    .child(
                        Button::new("session-menu")
                            .label(session_branch_label)
                            .max_w(px(300.))
                            .ghost()
                            .disabled(self.state.session_id.is_empty())
                            .on_click(cx.listener(|this, _, window, cx| {
                                this.toggle_session_menu(window, cx)
                            })),
                    ),
            )
            .child(
                h_flex()
                    .items_center()
                    .gap_2()
                    .child(div().size(px(7.)).rounded_full().bg(connection_color))
                    .child(
                        div()
                            .text_xs()
                            .font_medium()
                            .text_color(theme.muted_foreground)
                            .child(connection),
                    ),
            )
    }

    fn render_session_menu(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme();
        let can_manage = self.state.can_manage_session();
        let can_rename = can_manage
            && !self.session_name_input.read(cx).value().trim().is_empty()
            && self.session_name_input.read(cx).value().trim() != self.state.session_name;
        let branches = self.state.branches.clone();

        div()
            .w_full()
            .flex_shrink_0()
            .border_b_1()
            .border_color(theme.border)
            .bg(theme.secondary)
            .child(
                v_flex()
                    .w_full()
                    .max_w(px(620.))
                    .mx_auto()
                    .gap_4()
                    .px_5()
                    .py_4()
                    .child(
                        v_flex()
                            .gap_2()
                            .child(
                                div()
                                    .text_xs()
                                    .font_semibold()
                                    .text_color(theme.muted_foreground)
                                    .child("SESSION"),
                            )
                            .child(
                                h_flex()
                                    .w_full()
                                    .gap_2()
                                    .child(
                                        div()
                                            .min_w(px(0.))
                                            .flex_1()
                                            .child(Input::new(&self.session_name_input)),
                                    )
                                    .child(
                                        Button::new("rename-session")
                                            .label("Rename")
                                            .primary()
                                            .disabled(!can_rename)
                                            .on_click(cx.listener(|this, _, _, cx| {
                                                this.rename_session(cx)
                                            })),
                                    ),
                            ),
                    )
                    .child(
                        h_flex()
                            .w_full()
                            .items_center()
                            .justify_between()
                            .child(
                                div()
                                    .text_xs()
                                    .font_semibold()
                                    .text_color(theme.muted_foreground)
                                    .child("BRANCHES"),
                            )
                            .child(
                                Button::new("fork-branch")
                                    .label("Fork current")
                                    .ghost()
                                    .disabled(!can_manage || branches.is_empty())
                                    .on_click(cx.listener(|this, _, _, cx| this.fork_branch(cx))),
                            ),
                    )
                    .child(
                        v_flex()
                            .id("session-branches")
                            .max_h(px(PICKER_MAX_HEIGHT))
                            .overflow_y_scrollbar()
                            .gap_1()
                            .children(branches.into_iter().enumerate().map(|(index, branch)| {
                                let branch_id = branch.id.clone();
                                let branch_name = if branch.name.is_empty() {
                                    branch.id.clone()
                                } else {
                                    branch.name.clone()
                                };
                                h_flex()
                                    .w_full()
                                    .min_h(px(38.))
                                    .gap_3()
                                    .px_2()
                                    .items_center()
                                    .justify_between()
                                    .rounded_md()
                                    .when(branch.active, |row| row.bg(theme.accent))
                                    .child(
                                        v_flex()
                                            .min_w(px(0.))
                                            .flex_1()
                                            .child(
                                                div()
                                                    .text_sm()
                                                    .font_medium()
                                                    .overflow_hidden()
                                                    .text_ellipsis()
                                                    .child(if branch.active {
                                                        format!("✓ {branch_name}")
                                                    } else {
                                                        branch_name
                                                    }),
                                            )
                                            .child(
                                                div()
                                                    .text_xs()
                                                    .text_color(theme.muted_foreground)
                                                    .child(format!("{} messages", branch.messages)),
                                            ),
                                    )
                                    .child(
                                        Button::new(("select-branch", index))
                                            .label(if branch.active { "Current" } else { "Open" })
                                            .ghost()
                                            .disabled(!can_manage || branch.active)
                                            .on_click(cx.listener(move |this, _, _, cx| {
                                                this.select_branch(&branch_id, cx)
                                            })),
                                    )
                            })),
                    ),
            )
    }

    fn render_transcript(&self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme().clone();
        let transcript = if self.state.messages.is_empty() {
            v_flex()
                .w_full()
                .max_w(px(CONVERSATION_WIDTH))
                .flex_1()
                .items_center()
                .justify_center()
                .px_8()
                .child(self.render_empty_state(&theme))
                .into_any_element()
        } else {
            v_flex()
                .w_full()
                .max_w(px(CONVERSATION_WIDTH))
                .gap_6()
                .px_6()
                .py_8()
                .children(
                    self.state
                        .messages
                        .iter()
                        .enumerate()
                        .map(|(index, message)| {
                            self.render_message(index, message, &theme, window, cx)
                        }),
                )
                .into_any_element()
        };

        div()
            .id("transcript")
            .min_h(px(0.))
            .flex_1()
            .track_scroll(&self.scroll_handle)
            .overflow_y_scrollbar()
            .child(
                div()
                    .w_full()
                    .min_h_full()
                    .flex()
                    .justify_center()
                    .child(transcript),
            )
    }

    fn render_empty_state(&self, theme: &gpui_component::theme::Theme) -> impl IntoElement {
        v_flex()
            .max_w(px(440.))
            .items_center()
            .gap_2()
            .child(
                div()
                    .text_lg()
                    .font_semibold()
                    .child("What are we working on?"),
            )
            .child(
                div()
                    .text_center()
                    .text_sm()
                    .text_color(theme.muted_foreground)
                    .child(format!(
                        "Ask about {} or describe a change.",
                        self.project_label()
                    )),
            )
    }

    fn render_message(
        &self,
        _index: usize,
        message: &ChatMessage,
        theme: &gpui_component::theme::Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let render_id = message.render_id;
        let code_block_ordinal = Arc::new(AtomicUsize::new(0));
        match message.role {
            ChatRole::User => h_flex()
                .w_full()
                .justify_end()
                .child(
                    div()
                        .max_w(px(620.))
                        .px_4()
                        .py_3()
                        .rounded_xl()
                        .bg(theme.primary)
                        .text_color(theme.primary_foreground)
                        .text_sm()
                        .child(message.text.clone()),
                )
                .into_any_element(),
            ChatRole::Assistant => h_flex()
                .w_full()
                .items_start()
                .gap_3()
                .child(
                    div()
                        .size(px(28.))
                        .flex_shrink_0()
                        .flex()
                        .items_center()
                        .justify_center()
                        .rounded_lg()
                        .border_1()
                        .border_color(theme.border)
                        .bg(theme.secondary)
                        .text_xs()
                        .font_semibold()
                        .child("S"),
                )
                .child(
                    v_flex()
                        .min_w(px(0.))
                        .flex_1()
                        .gap_2()
                        .child(
                            h_flex()
                                .items_center()
                                .gap_2()
                                .child(div().text_xs().font_semibold().child("Snow"))
                                .when(message.streaming, |row| {
                                    row.child(
                                        div().text_xs().text_color(theme.info).child("Working…"),
                                    )
                                }),
                        )
                        .child(
                            TextView::markdown(
                                ("assistant-markdown", render_id),
                                message.text.clone(),
                                window,
                                cx,
                            )
                            .selectable(true)
                            .code_block_actions(
                                move |code_block, _, _| {
                                    let code = code_block.code();
                                    let ordinal =
                                        code_block_ordinal.fetch_add(1, Ordering::Relaxed);
                                    let action_id = (render_id << 32) | ordinal as u64;
                                    Button::new(("copy-code", action_id))
                                        .label("Copy")
                                        .ghost()
                                        .on_click(move |_, _, cx| {
                                            cx.write_to_clipboard(ClipboardItem::new_string(
                                                code.to_string(),
                                            ));
                                        })
                                },
                            ),
                        ),
                )
                .into_any_element(),
            ChatRole::System => h_flex()
                .w_full()
                .justify_center()
                .child(
                    div()
                        .px_3()
                        .py_1()
                        .rounded_full()
                        .bg(theme.secondary)
                        .text_xs()
                        .text_color(theme.muted_foreground)
                        .child(message.text.clone()),
                )
                .into_any_element(),
        }
    }

    fn render_contextual_activity(&self, theme: &gpui_component::theme::Theme) -> AnyElement {
        let hidden = self.state.tools.len().saturating_sub(3);
        v_flex()
            .w_full()
            .gap_1()
            .px_1()
            .when(hidden > 0, |list| {
                list.child(
                    div()
                        .px_2()
                        .text_xs()
                        .text_color(theme.muted_foreground)
                        .child(format!("+{hidden} earlier")),
                )
            })
            .children(self.state.tools.iter().rev().take(3).map(|tool| {
                let status_color = match tool.state {
                    ToolState::Running => theme.info,
                    ToolState::Completed => theme.success,
                    ToolState::Failed => theme.danger,
                };
                h_flex()
                    .w_full()
                    .min_w(px(0.))
                    .items_center()
                    .gap_2()
                    .px_2()
                    .py_1()
                    .child(div().size(px(6.)).rounded_full().bg(status_color))
                    .child(
                        div()
                            .max_w(px(180.))
                            .overflow_hidden()
                            .text_ellipsis()
                            .text_xs()
                            .font_medium()
                            .child(tool.name.clone()),
                    )
                    .child(
                        div()
                            .min_w(px(0.))
                            .flex_1()
                            .overflow_hidden()
                            .text_ellipsis()
                            .text_xs()
                            .text_color(if tool.state == ToolState::Failed {
                                theme.danger
                            } else {
                                theme.muted_foreground
                            })
                            .child(tool.status.clone()),
                    )
            }))
            .into_any_element()
    }

    fn render_active_picker(&self, cx: &mut Context<Self>) -> AnyElement {
        match self.composer_picker.active {
            Some(ComposerPicker::Provider) => self.render_provider_picker(cx),
            Some(ComposerPicker::Model) => self.render_model_picker(cx),
            Some(ComposerPicker::Thinking) => self.render_thinking_picker(cx),
            None => div().into_any_element(),
        }
    }

    fn render_provider_picker(&self, cx: &mut Context<Self>) -> AnyElement {
        let theme = cx.theme();
        let can_switch = self.state.can_switch_provider();

        v_flex()
            .w(px(240.))
            .max_h(px(PICKER_MAX_HEIGHT))
            .overflow_y_scrollbar()
            .p_2()
            .gap_1()
            .rounded_xl()
            .border_1()
            .border_color(theme.border)
            .bg(theme.background)
            .children(PROVIDER_CHOICES.iter().enumerate().map(|(index, choice)| {
                let provider = choice.id;
                let selected = provider == self.provider;
                let retries_failure =
                    selected && matches!(self.state.connection, ConnectionState::Failed(_));
                if selected && !retries_failure {
                    h_flex()
                        .w_full()
                        .justify_between()
                        .px_3()
                        .py_2()
                        .rounded_lg()
                        .bg(theme.secondary)
                        .text_sm()
                        .font_medium()
                        .child(choice.label)
                        .child(
                            div()
                                .text_xs()
                                .text_color(theme.muted_foreground)
                                .child("Selected"),
                        )
                        .into_any_element()
                } else {
                    Button::new(("provider-picker", index))
                        .w_full()
                        .label(choice.label)
                        .ghost()
                        .disabled(!can_switch)
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.select_provider(provider, cx);
                        }))
                        .into_any_element()
                }
            }))
            .into_any_element()
    }

    fn render_model_picker(&self, cx: &mut Context<Self>) -> AnyElement {
        let theme = cx.theme();
        let can_switch = self.state.can_switch_model();

        v_flex()
            .w(px(320.))
            .max_h(px(PICKER_MAX_HEIGHT))
            .overflow_y_scrollbar()
            .p_2()
            .gap_1()
            .rounded_xl()
            .border_1()
            .border_color(theme.border)
            .bg(theme.background)
            .children(self.state.models.iter().enumerate().map(|(index, model)| {
                let model_id = model.id.clone();
                let selected = model.id == self.state.current_model;
                let label = if model.display_name.is_empty() {
                    model.id.clone()
                } else {
                    model.display_name.clone()
                };
                if selected {
                    h_flex()
                        .w_full()
                        .justify_between()
                        .px_3()
                        .py_2()
                        .rounded_lg()
                        .bg(theme.secondary)
                        .text_sm()
                        .font_medium()
                        .child(label)
                        .child(
                            div()
                                .text_xs()
                                .text_color(theme.muted_foreground)
                                .child("Selected"),
                        )
                        .into_any_element()
                } else {
                    Button::new(("model-picker", index))
                        .w_full()
                        .label(label)
                        .ghost()
                        .disabled(!can_switch)
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.select_model(&model_id, cx);
                        }))
                        .into_any_element()
                }
            }))
            .into_any_element()
    }

    fn render_thinking_picker(&self, cx: &mut Context<Self>) -> AnyElement {
        let theme = cx.theme();
        let can_switch = self.state.can_switch_thinking();

        v_flex()
            .w(px(240.))
            .max_h(px(PICKER_MAX_HEIGHT))
            .overflow_y_scrollbar()
            .p_2()
            .gap_1()
            .rounded_xl()
            .border_1()
            .border_color(theme.border)
            .bg(theme.background)
            .children(
                self.state
                    .thinking_levels
                    .iter()
                    .enumerate()
                    .map(|(index, level)| {
                        let target = level.clone();
                        let selected = level == &self.state.current_thinking;
                        if selected {
                            h_flex()
                                .w_full()
                                .justify_between()
                                .px_3()
                                .py_2()
                                .rounded_lg()
                                .bg(theme.secondary)
                                .text_sm()
                                .font_medium()
                                .child(thinking_label(level))
                                .child(
                                    div()
                                        .text_xs()
                                        .text_color(theme.muted_foreground)
                                        .child("Selected"),
                                )
                                .into_any_element()
                        } else {
                            Button::new(("thinking-picker", index))
                                .w_full()
                                .label(thinking_label(level))
                                .ghost()
                                .disabled(!can_switch)
                                .on_click(cx.listener(move |this, _, _, cx| {
                                    this.select_thinking(&target, cx);
                                }))
                                .into_any_element()
                        }
                    }),
            )
            .into_any_element()
    }

    fn render_composer(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let active_picker = self
            .composer_picker
            .active
            .map(|_| self.render_active_picker(cx));
        let theme = cx.theme();
        let can_send = self.state.can_send();
        let can_abort = self.state.can_abort();
        let can_switch_provider = self.state.can_switch_provider();
        let can_open_model_picker = self.state.can_send() && !self.state.models.is_empty();
        let can_open_thinking_picker = self.state.can_switch_thinking();

        v_flex()
            .w_full()
            .flex_shrink_0()
            .items_center()
            .px_5()
            .pb_5()
            .pt_2()
            .child(
                v_flex()
                    .w_full()
                    .max_w(px(CONVERSATION_WIDTH))
                    .gap_2()
                    .when_some(self.state.last_error.as_ref(), |composer, error| {
                        composer.child(
                            h_flex()
                                .w_full()
                                .px_3()
                                .py_2()
                                .rounded_lg()
                                .border_1()
                                .border_color(theme.danger)
                                .text_xs()
                                .text_color(theme.danger)
                                .child(error.clone()),
                        )
                    })
                    .when(!self.state.tools.is_empty(), |composer| {
                        composer.child(self.render_contextual_activity(theme))
                    })
                    .when_some(active_picker, |composer, picker| composer.child(picker))
                    .child(
                        v_flex()
                            .w_full()
                            .gap_2()
                            .p_3()
                            .rounded_xl()
                            .border_1()
                            .border_color(theme.border)
                            .bg(theme.secondary)
                            .child(
                                Input::new(&self.input)
                                    .w_full()
                                    .appearance(false)
                                    .disabled(!can_send),
                            )
                            .child(
                                h_flex()
                                    .w_full()
                                    .items_center()
                                    .justify_between()
                                    .gap_3()
                                    .child(
                                        h_flex()
                                            .min_w(px(0.))
                                            .items_center()
                                            .gap_1()
                                            .child(
                                                Button::new("provider-menu")
                                                    .label(format!("{}  ▾", self.provider_label()))
                                                    .ghost()
                                                    .disabled(!can_switch_provider)
                                                    .on_click(cx.listener(|this, _, _, cx| {
                                                        this.toggle_picker(
                                                            ComposerPicker::Provider,
                                                            cx,
                                                        )
                                                    })),
                                            )
                                            .child(
                                                Button::new("model-menu")
                                                    .label(format!("{}  ▾", self.model_label()))
                                                    .ghost()
                                                    .disabled(!can_open_model_picker)
                                                    .on_click(cx.listener(|this, _, _, cx| {
                                                        this.toggle_picker(
                                                            ComposerPicker::Model,
                                                            cx,
                                                        )
                                                    })),
                                            )
                                            .child(
                                                Button::new("thinking-menu")
                                                    .label(format!(
                                                        "Thinking: {}  ▾",
                                                        thinking_label(
                                                            &self.state.current_thinking
                                                        )
                                                    ))
                                                    .ghost()
                                                    .disabled(!can_open_thinking_picker)
                                                    .on_click(cx.listener(|this, _, _, cx| {
                                                        this.toggle_picker(
                                                            ComposerPicker::Thinking,
                                                            cx,
                                                        )
                                                    })),
                                            ),
                                    )
                                    .child(match self.state.composer_action() {
                                        ComposerAction::Stop => Button::new("stop")
                                            .danger()
                                            .label("Stop")
                                            .disabled(!can_abort)
                                            .on_click(cx.listener(|this, _, _, cx| this.abort(cx)))
                                            .into_any_element(),
                                        ComposerAction::Send => Button::new("send")
                                            .primary()
                                            .label("Send")
                                            .disabled(!can_send)
                                            .on_click(cx.listener(|this, _, window, cx| {
                                                this.submit(window, cx)
                                            }))
                                            .into_any_element(),
                                    }),
                            ),
                    ),
            )
    }

    fn connection_label(&self, theme: &gpui_component::theme::Theme) -> (&'static str, gpui::Hsla) {
        match &self.state.connection {
            ConnectionState::Starting => ("Starting", theme.info),
            ConnectionState::Ready { .. } => ("Connected", theme.success),
            ConnectionState::Stopping => ("Stopping", theme.warning),
            ConnectionState::Stopped => ("Stopped", theme.muted_foreground),
            ConnectionState::Failed(_) => ("Failed", theme.danger),
        }
    }

    fn project_label(&self) -> String {
        project_name(&self.state.project_cwd)
            .or_else(|| {
                self.runtime_config.as_ref().and_then(|config| {
                    config
                        .project_root
                        .file_name()
                        .and_then(|name| name.to_str())
                        .filter(|name| !name.is_empty())
                        .map(str::to_owned)
                })
            })
            .unwrap_or_else(|| "Current project".into())
    }

    fn provider_label(&self) -> String {
        PROVIDER_CHOICES
            .iter()
            .find(|choice| choice.id == self.provider)
            .map(|choice| choice.label.to_owned())
            .unwrap_or_else(|| self.provider.clone())
    }

    fn model_label(&self) -> String {
        self.state
            .models
            .iter()
            .find(|model| model.id == self.state.current_model)
            .map(|model| {
                if model.display_name.is_empty() {
                    model.id.clone()
                } else {
                    model.display_name.clone()
                }
            })
            .filter(|model| !model.is_empty())
            .unwrap_or_else(|| {
                if self.state.current_model.is_empty() {
                    "Loading model…".into()
                } else {
                    self.state.current_model.clone()
                }
            })
    }
}
