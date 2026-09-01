use gpui::Focusable as _;

use super::*;

const CONVERSATION_WIDTH: f32 = 920.;
const PICKER_MAX_HEIGHT: f32 = 260.;
const MANAGEMENT_SESSION_ROW_HEIGHT: f32 = 64.;

fn short_id(id: &str) -> &str {
    &id[..id.len().min(8)]
}

fn display_value(value: &str) -> &str {
    if value.is_empty() { "—" } else { value }
}

fn domain_element_id(namespace: &str, domain_id: &str) -> SharedString {
    format!("{namespace}-{domain_id}").into()
}

fn plain_text_html(value: &str) -> String {
    let mut escaped = String::with_capacity(value.len());
    for character in value.chars() {
        match character {
            '&' => escaped.push_str("&amp;"),
            '<' => escaped.push_str("&lt;"),
            '>' => escaped.push_str("&gt;"),
            '"' => escaped.push_str("&quot;"),
            '\'' => escaped.push_str("&#39;"),
            '\n' => escaped.push_str("<br>"),
            _ => escaped.push(character),
        }
    }
    escaped
}

fn ordered_branch_topology(branches: &[SessionBranch]) -> Vec<(SessionBranch, usize)> {
    fn visit(
        index: usize,
        depth: usize,
        branches: &[SessionBranch],
        visited: &mut [bool],
        ordered: &mut Vec<(SessionBranch, usize)>,
    ) {
        if visited[index] {
            return;
        }
        visited[index] = true;
        ordered.push((branches[index].clone(), depth));
        let parent_id = branches[index].id.as_str();
        for (child_index, child) in branches.iter().enumerate() {
            if !visited[child_index] && child.parent_branch_id == parent_id {
                visit(child_index, depth + 1, branches, visited, ordered);
            }
        }
    }

    let mut visited = vec![false; branches.len()];
    let mut ordered = Vec::with_capacity(branches.len());
    for (index, branch) in branches.iter().enumerate() {
        let parent_exists = branches
            .iter()
            .any(|candidate| candidate.id == branch.parent_branch_id);
        if branch.parent_branch_id.is_empty()
            || branch.parent_branch_id == branch.id
            || !parent_exists
        {
            visit(index, 0, branches, &mut visited, &mut ordered);
        }
    }
    // Corrupt or cyclic catalogs still remain visible instead of disappearing.
    for (index, _) in branches.iter().enumerate() {
        visit(index, 0, branches, &mut visited, &mut ordered);
    }
    ordered
}

impl Workspace {
    pub(super) fn render_workspace(
        &self,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let centered_empty = self.canvas_layout() == WorkspaceCanvasLayout::CenteredEmpty;
        let management_panels = self.visible_management_panels();
        let settings_layout =
            settings_workspace_layout(management_panels.settings, self.settings_panel.is_some());
        let settings_workspace_open = settings_layout != SettingsWorkspaceLayout::Hidden;
        let settings_page = (settings_layout == SettingsWorkspaceLayout::Ready)
            .then_some(self.settings_panel.as_ref())
            .flatten();
        let workspace_context = if settings_workspace_open {
            "DesktopSettings"
        } else if self.state.active_prompt.is_none() {
            "DesktopWorkspace DesktopWorkspaceIdle"
        } else {
            "DesktopWorkspace"
        };
        let workspace_width = window
            .inner_window_bounds()
            .get_bounds()
            .size
            .width
            .to_f64() as f32;
        let composer_footer_layout =
            composer_footer_layout(workspace_width, self.sidebar_collapsed);
        let top_bar_layout = workspace_top_bar_layout(workspace_width, self.sidebar_collapsed);
        let main = v_flex()
            .min_w(px(0.))
            .min_h(px(0.))
            .flex_1()
            .h_full()
            .bg(cx.theme().background)
            .child(self.render_top_bar(top_bar_layout, cx))
            .when(management_panels.sessions, |workspace| {
                workspace.child(self.render_sessions_panel(cx))
            })
            .when(management_panels.processes, |workspace| {
                workspace.child(self.render_processes_panel(cx))
            })
            .when(management_panels.subagents, |workspace| {
                workspace.child(self.render_subagents_panel(cx))
            })
            .when_some(
                management_panels
                    .resource
                    .then_some(self.resource_panel.as_ref())
                    .flatten(),
                |workspace, panel| workspace.child(self.render_resource_panel(panel, cx)),
            )
            .when(management_panels.auth, |workspace| {
                workspace.child(self.render_auth_panel(cx))
            })
            .when(centered_empty, |workspace| {
                workspace.child(
                    v_flex()
                        .min_h(px(0.))
                        .flex_1()
                        .items_center()
                        .justify_center()
                        .gap_4()
                        .pt_8()
                        .child(self.render_empty_state(cx.theme()))
                        .when(self.plan_nudge_visible(cx), |canvas| {
                            canvas.child(self.render_plan_nudge(cx))
                        })
                        .child(self.render_composer(composer_footer_layout, cx)),
                )
            })
            .when(!centered_empty, |workspace| {
                workspace
                    .child(self.render_transcript(window, cx))
                    .when(self.state.plan_review_ready, |workspace| {
                        workspace.child(self.render_plan_review(cx))
                    })
                    .when(self.plan_nudge_visible(cx), |workspace| {
                        workspace.child(self.render_plan_nudge(cx))
                    })
                    .when_some(self.render_interaction_card(cx), |workspace, card| {
                        workspace.child(card)
                    })
                    .child(self.render_composer(composer_footer_layout, cx))
            });

        h_flex()
            .key_context(workspace_context)
            .on_action(cx.listener(Self::picker_up))
            .on_action(cx.listener(Self::picker_down))
            .on_action(cx.listener(Self::dismiss_picker))
            .on_action(cx.listener(Self::history_previous))
            .on_action(cx.listener(Self::history_next))
            .on_action(cx.listener(Self::composer_tab))
            .on_action(cx.listener(Self::composer_back_tab))
            .on_action(cx.listener(Self::paste_composer))
            .on_action(cx.listener(Self::semantic_submit))
            .on_action(cx.listener(Self::semantic_follow_up))
            .on_action(cx.listener(Self::semantic_newline))
            .on_action(cx.listener(Self::semantic_paste))
            .on_action(cx.listener(Self::semantic_abort))
            .on_action(cx.listener(Self::semantic_quit))
            .on_action(cx.listener(Self::semantic_toggle_mode))
            .on_action(cx.listener(Self::semantic_thinking))
            .on_action(cx.listener(Self::semantic_models))
            .on_action(cx.listener(Self::semantic_agents))
            .on_action(cx.listener(Self::semantic_processes))
            .on_action(cx.listener(Self::semantic_page_up))
            .on_action(cx.listener(Self::semantic_page_down))
            .on_action(cx.listener(Self::semantic_top))
            .on_action(cx.listener(Self::semantic_bottom))
            .on_action(cx.listener(Self::semantic_line_up))
            .on_action(cx.listener(Self::semantic_line_down))
            .on_action(cx.listener(Self::semantic_picker_up))
            .on_action(cx.listener(Self::semantic_picker_down))
            .on_action(cx.listener(Self::semantic_picker_previous))
            .on_action(cx.listener(Self::semantic_picker_next))
            .on_action(cx.listener(Self::semantic_picker_page_up))
            .on_action(cx.listener(Self::semantic_picker_page_down))
            .on_action(cx.listener(Self::semantic_picker_top))
            .on_action(cx.listener(Self::semantic_picker_bottom))
            .on_action(cx.listener(Self::semantic_accept))
            .on_action(cx.listener(Self::semantic_close))
            .on_action(cx.listener(Self::semantic_branch_fork))
            .on_action(cx.listener(Self::semantic_branch_rename))
            .on_action(cx.listener(Self::semantic_branch_delete))
            .on_action(cx.listener(Self::semantic_confirm))
            .size_full()
            .min_w(px(900.))
            .min_h(px(600.))
            .overflow_hidden()
            .bg(cx.theme().background)
            .text_color(cx.theme().foreground)
            .when(settings_workspace_open, |workspace| {
                let workspace = workspace.child(self.render_settings_sidebar(cx));
                if let Some(settings) = settings_page {
                    workspace.child(self.render_settings_panel(settings, cx))
                } else {
                    workspace.child(self.render_settings_loading(cx))
                }
            })
            .when(!settings_workspace_open, |workspace| {
                workspace.child(self.render_sidebar(cx)).child(main)
            })
    }

    fn plan_nudge_visible(&self, cx: &App) -> bool {
        if self.plan_nudge_dismissed
            || !self.state.can_send()
            || self.state.collaboration_mode != "default"
        {
            return false;
        }
        let input = self.input.read(cx).value().trim().to_lowercase();
        if input.starts_with('/') {
            return false;
        }
        input
            .split(|character: char| !(character.is_ascii_alphanumeric() || character == '_'))
            .any(|word| word == "plan")
    }

    fn render_plan_nudge(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme();
        h_flex()
            .w_full()
            .flex_shrink_0()
            .justify_center()
            .px_5()
            .pt_2()
            .child(
                h_flex()
                    .w_full()
                    .max_w(px(CONVERSATION_WIDTH))
                    .items_center()
                    .justify_between()
                    .gap_3()
                    .p_3()
                    .rounded_lg()
                    .border_1()
                    .border_color(theme.border)
                    .bg(theme.secondary)
                    .child(
                        div()
                            .text_sm()
                            .child("Planning a change? Use Plan mode to inspect before editing."),
                    )
                    .child(
                        h_flex()
                            .gap_1()
                            .child(
                                Button::new("enable-plan-nudge")
                                    .label("Use Plan mode")
                                    .primary()
                                    .on_click(
                                        cx.listener(|this, _, _, cx| this.enable_plan_mode(cx)),
                                    ),
                            )
                            .child(
                                Button::new("dismiss-plan-nudge")
                                    .label("Not now")
                                    .ghost()
                                    .on_click(
                                        cx.listener(|this, _, _, cx| this.dismiss_plan_nudge(cx)),
                                    ),
                            ),
                    ),
            )
    }

    fn render_plan_review(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme();
        v_flex()
            .w_full()
            .flex_shrink_0()
            .items_center()
            .px_5()
            .pt_2()
            .child(
                v_flex()
                    .w_full()
                    .max_w(px(CONVERSATION_WIDTH))
                    .gap_2()
                    .p_3()
                    .rounded_lg()
                    .border_1()
                    .border_color(theme.border)
                    .bg(theme.secondary)
                    .child(
                        div()
                            .text_sm()
                            .font_semibold()
                            .child("Implement this plan?"),
                    )
                    .child(
                        h_flex()
                            .flex_wrap()
                            .gap_2()
                            .child(
                                Button::new("implement-plan-current")
                                    .label("Implement in this context")
                                    .primary()
                                    .on_click(cx.listener(|this, _, window, cx| {
                                        this.implement_latest_plan(false, window, cx)
                                    })),
                            )
                            .child(
                                Button::new("implement-plan-fresh")
                                    .label("Clear context and implement")
                                    .on_click(cx.listener(|this, _, window, cx| {
                                        this.implement_latest_plan(true, window, cx)
                                    })),
                            )
                            .child(
                                Button::new("stay-plan")
                                    .label("Stay in Plan mode")
                                    .ghost()
                                    .on_click(
                                        cx.listener(|this, _, _, cx| this.stay_in_plan_mode(cx)),
                                    ),
                            ),
                    ),
            )
    }

    fn render_sidebar(&self, cx: &mut Context<Self>) -> AnyElement {
        let theme = cx.theme().clone();
        let can_change_session = self.state.can_send()
            && self.state.active_interaction.is_none()
            && !self.has_pending_session_transition();

        if self.sidebar_collapsed {
            return v_flex()
                .w(px(COLLAPSED_SIDEBAR_WIDTH))
                .h_full()
                .flex_shrink_0()
                .items_center()
                .gap_2()
                .py_3()
                .border_r_1()
                .border_color(theme.border)
                .bg(theme.secondary)
                .child(
                    div()
                        .size(px(30.))
                        .flex()
                        .items_center()
                        .justify_center()
                        .rounded_lg()
                        .bg(theme.primary)
                        .text_color(theme.primary_foreground)
                        .font_semibold()
                        .child("S"),
                )
                .child(
                    Button::new("collapsed-new-session")
                        .icon(IconName::Plus)
                        .tooltip("New thread")
                        .primary()
                        .disabled(!can_change_session)
                        .on_click(
                            cx.listener(|this, _, window, cx| this.create_session(window, cx)),
                        ),
                )
                .child(div().flex_1())
                .child(
                    Button::new("collapsed-settings")
                        .label("⚙︎")
                        .tooltip("Settings")
                        .ghost()
                        .on_click(cx.listener(|this, _, window, cx| {
                            this.open_settings_section(SettingsSection::General, window, cx)
                        })),
                )
                .into_any_element();
        }

        let sessions = if self.sessions.is_empty() {
            v_flex()
                .w_full()
                .flex_1()
                .items_center()
                .justify_center()
                .px_4()
                .child(
                    div()
                        .text_xs()
                        .text_color(theme.muted_foreground)
                        .child("No threads yet"),
                )
                .into_any_element()
        } else {
            let workspace = cx.entity().downgrade();
            let scroll_handle = self.sidebar_session_list.clone();
            let session_rows = uniform_list(
                "sidebar-session-list",
                self.sessions.len(),
                move |visible_range, _window, cx| {
                    workspace
                        .update(cx, |this, cx| {
                            let theme = cx.theme();
                            visible_range
                                .filter_map(|index| {
                                    let session = this.sessions.get(index)?;
                                    let session_id = session.session_id.clone();
                                    let row_id = domain_element_id("sidebar-session", &session_id);
                                    let active = session.active;
                                    let title = sidebar_session_title(&session.name);
                                    Some(
                                        Button::new(row_id)
                                            .w_full()
                                            .min_w(px(0.))
                                            .h(px(40.))
                                            .px_3()
                                            .overflow_hidden()
                                            .justify_start()
                                            .ghost()
                                            .when(active, |button| {
                                                button
                                                    .bg(theme.primary)
                                                    .text_color(theme.primary_foreground)
                                            })
                                            .disabled(!can_change_session)
                                            .child(
                                                div()
                                                    .w_full()
                                                    .min_w(px(0.))
                                                    .overflow_hidden()
                                                    .text_ellipsis()
                                                    .text_sm()
                                                    .when(active, |title| title.font_medium())
                                                    .child(title),
                                            )
                                            .on_click(cx.listener(move |this, _, window, cx| {
                                                if !active {
                                                    this.open_session(&session_id, window, cx);
                                                }
                                            }))
                                            .into_any_element(),
                                    )
                                })
                                .collect::<Vec<_>>()
                        })
                        .unwrap_or_default()
                },
            )
            .track_scroll(scroll_handle.clone())
            .w_full()
            .h_full();
            div()
                .w_full()
                .min_h(px(0.))
                .flex_1()
                .px_2()
                .pb_2()
                .overflow_hidden()
                .child(session_rows)
                .vertical_scrollbar(&scroll_handle)
                .into_any_element()
        };

        v_flex()
            .w(px(EXPANDED_SIDEBAR_WIDTH))
            .h_full()
            .flex_shrink_0()
            .border_r_1()
            .border_color(theme.border)
            .bg(theme.secondary)
            .child(
                h_flex()
                    .h(px(54.))
                    .w_full()
                    .flex_shrink_0()
                    .items_center()
                    .justify_between()
                    .px_4()
                    .child(
                        h_flex()
                            .gap_2()
                            .items_center()
                            .child(
                                div()
                                    .size(px(28.))
                                    .flex()
                                    .items_center()
                                    .justify_center()
                                    .rounded_lg()
                                    .bg(theme.primary)
                                    .text_color(theme.primary_foreground)
                                    .font_semibold()
                                    .child("S"),
                            )
                            .child(div().text_sm().font_semibold().child("Snow")),
                    ),
            )
            .child(
                div().px_3().pb_3().child(
                    Button::new("sidebar-new-session")
                        .w_full()
                        .icon(IconName::Plus)
                        .label("New thread")
                        .primary()
                        .disabled(!can_change_session)
                        .on_click(
                            cx.listener(|this, _, window, cx| this.create_session(window, cx)),
                        ),
                ),
            )
            .child(sessions)
            .child(
                div()
                    .w_full()
                    .flex_shrink_0()
                    .p_3()
                    .border_t_1()
                    .border_color(theme.border)
                    .child(
                        Button::new("sidebar-settings")
                            .w_full()
                            .label("Settings")
                            .justify_start()
                            .ghost()
                            .on_click(cx.listener(|this, _, window, cx| {
                                this.open_settings_section(SettingsSection::General, window, cx)
                            })),
                    ),
            )
            .into_any_element()
    }

    fn render_top_bar(
        &self,
        layout: WorkspaceTopBarLayout,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let theme = cx.theme().clone();
        let compact = layout == WorkspaceTopBarLayout::Compact;
        let plan_mode = self.state.collaboration_mode == "plan";
        let session_name = if self.state.session_name.is_empty() {
            "New thread"
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
        let can_change_session = self.state.can_send()
            && self.state.active_interaction.is_none()
            && !self.has_pending_session_transition();

        h_flex()
            .h(px(54.))
            .w_full()
            .flex_shrink_0()
            .px_4()
            .items_center()
            .justify_between()
            .border_b_1()
            .border_color(theme.border)
            .child(
                h_flex()
                    .min_w(px(0.))
                    .items_center()
                    .gap_2()
                    .child(
                        Button::new("toolbar-sidebar-toggle")
                            .label(if self.sidebar_collapsed { "☰" } else { "‹" })
                            .tooltip(if self.sidebar_collapsed {
                                "Expand sidebar"
                            } else {
                                "Collapse sidebar"
                            })
                            .ghost()
                            .on_click(cx.listener(|this, _, _, cx| this.toggle_sidebar(cx))),
                    )
                    .when(!compact, |bar| {
                        bar.child(
                            div()
                                .min_w(px(0.))
                                .max_w(px(250.))
                                .overflow_hidden()
                                .text_ellipsis()
                                .text_sm()
                                .font_medium()
                                .child(self.project_label()),
                        )
                        .child(
                            div()
                                .text_xs()
                                .text_color(theme.muted_foreground)
                                .child("/"),
                        )
                    })
                    .child({
                        let disabled = self.state.session_id.is_empty()
                            || self.state.active_interaction.is_some();
                        if disabled {
                            Button::new("session-menu-trigger-disabled")
                                .label(session_branch_label)
                                .max_w(px(if compact { 260. } else { 360. }))
                                .ghost()
                                .disabled(true)
                                .into_any_element()
                        } else {
                            let workspace = cx.entity().downgrade();
                            Popover::new("session-menu-popover")
                                .anchor(Corner::TopLeft)
                                .open(self.session_menu_open)
                                .on_open_change(move |open, window, cx| {
                                    let _ = workspace.update(cx, |this, cx| {
                                        this.set_session_menu_open(*open, window, cx)
                                    });
                                })
                                .trigger(
                                    Button::new("session-menu-trigger")
                                        .label(session_branch_label)
                                        .max_w(px(if compact { 260. } else { 360. }))
                                        .ghost(),
                                )
                                .child(self.render_session_menu(cx))
                                .into_any_element()
                        }
                    }),
            )
            .child(
                h_flex()
                    .items_center()
                    .gap_1()
                    .when(self.state.pending_inputs.total > 0, |bar| {
                        bar.child(
                            div()
                                .px_2()
                                .rounded_full()
                                .bg(theme.secondary)
                                .text_xs()
                                .text_color(theme.muted_foreground)
                                .child(format!("{} queued", self.state.pending_inputs.total)),
                        )
                    })
                    .child(
                        Button::new("collaboration-mode")
                            .label(if plan_mode { "Plan" } else { "Default" })
                            .tooltip(if plan_mode {
                                "Plan mode active"
                            } else {
                                "Default mode active"
                            })
                            .selected(plan_mode)
                            .when(plan_mode, |button| button.bg(theme.secondary))
                            .ghost()
                            .disabled(self.state.active_interaction.is_some())
                            .on_click(
                                cx.listener(|this, _, _, cx| this.toggle_collaboration_mode(cx)),
                            ),
                    )
                    .when(self.state.messages.is_empty(), |bar| {
                        bar.child(
                            Button::new("initialize-project")
                                .label("Initialize")
                                .ghost()
                                .disabled(!can_change_session)
                                .on_click(cx.listener(|this, _, _, cx| {
                                    this.run_rpc_command(RpcCommand {
                                        name: "project_init".into(),
                                        fields: serde_json::Map::new(),
                                        refresh_runtime: false,
                                    });
                                    cx.notify();
                                })),
                        )
                    }),
            )
    }

    fn render_settings_sidebar(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme().clone();
        let (connection, connection_color) = self.connection_label(&theme);

        v_flex()
            .w(px(236.))
            .h_full()
            .flex_shrink_0()
            .border_r_1()
            .border_color(theme.border)
            .bg(theme.secondary)
            .child(
                h_flex()
                    .h(px(58.))
                    .flex_shrink_0()
                    .items_center()
                    .gap_3()
                    .px_4()
                    .border_b_1()
                    .border_color(theme.border)
                    .child(
                        div()
                            .size(px(30.))
                            .flex()
                            .items_center()
                            .justify_center()
                            .rounded_lg()
                            .bg(theme.primary)
                            .text_color(theme.primary_foreground)
                            .font_semibold()
                            .child("S"),
                    )
                    .child(
                        v_flex()
                            .child(div().text_sm().font_semibold().child("Snow"))
                            .child(
                                div()
                                    .text_xs()
                                    .text_color(theme.muted_foreground)
                                    .child("Settings"),
                            ),
                    ),
            )
            .child(
                v_flex()
                    .gap_1()
                    .p_3()
                    .child(
                        div()
                            .px_2()
                            .pb_1()
                            .text_xs()
                            .text_color(theme.muted_foreground)
                            .child("Preferences"),
                    )
                    .children(SettingsSection::ALL.into_iter().map(|section| {
                        let selected = self.settings_section == section;
                        Button::new(domain_element_id("settings-section", section.id()))
                            .w_full()
                            .label(section.label())
                            .justify_start()
                            .ghost()
                            .selected(selected)
                            .when(selected, |button| button.bg(theme.background))
                            .on_click(cx.listener(move |this, _, window, cx| {
                                this.select_settings_section(section, window, cx)
                            }))
                    })),
            )
            .child(div().flex_1())
            .child(
                v_flex()
                    .gap_2()
                    .p_3()
                    .border_t_1()
                    .border_color(theme.border)
                    .child(
                        h_flex()
                            .items_center()
                            .gap_2()
                            .px_2()
                            .child(div().size(px(7.)).rounded_full().bg(connection_color))
                            .child(
                                div()
                                    .text_xs()
                                    .text_color(theme.muted_foreground)
                                    .child(connection),
                            ),
                    ),
            )
    }

    fn render_settings_loading(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme().clone();

        v_flex()
            .track_focus(&self.settings_focus_handle)
            .min_w(px(0.))
            .min_h(px(0.))
            .flex_1()
            .h_full()
            .bg(theme.background)
            .child(
                h_flex()
                    .h(px(58.))
                    .w_full()
                    .flex_shrink_0()
                    .items_center()
                    .justify_between()
                    .gap_3()
                    .px_6()
                    .border_b_1()
                    .border_color(theme.border)
                    .child(
                        h_flex()
                            .items_center()
                            .gap_3()
                            .child(
                                div()
                                    .text_sm()
                                    .text_color(theme.muted_foreground)
                                    .child("Settings"),
                            )
                            .child(
                                div()
                                    .text_sm()
                                    .text_color(theme.muted_foreground)
                                    .child("/"),
                            )
                            .child(
                                div()
                                    .text_sm()
                                    .font_semibold()
                                    .child(self.settings_section.label()),
                            ),
                    )
                    .child(
                        Button::new("close-loading-settings")
                            .label("Done")
                            .ghost()
                            .on_click(cx.listener(|this, _, window, cx| {
                                this.close_settings_panel(window, cx)
                            })),
                    ),
            )
            .child(
                v_flex()
                    .flex_1()
                    .items_center()
                    .justify_center()
                    .gap_3()
                    .px_8()
                    .child(div().text_lg().font_semibold().child("Loading settings…"))
                    .child(
                        div()
                            .max_w(px(420.))
                            .text_sm()
                            .text_color(theme.muted_foreground)
                            .child("Snow is loading the canonical runtime settings and presentation resources."),
                    )
                    .child(
                        Button::new("retry-loading-settings")
                            .label("Retry")
                            .ghost()
                            .on_click(cx.listener(|this, _, _, cx| {
                                this.refresh_settings(cx)
                            })),
                    ),
            )
    }

    fn render_settings_panel(
        &self,
        settings: &Settings,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let theme = cx.theme().clone();
        let debug_enabled = settings.debug_enabled;
        let (settings_provider, settings_model) =
            presented_provider_model(&settings.provider, &settings.model);
        let (selected_appearance, appearance_diagnostic) = {
            let appearance = cx.global::<AppearanceState>();
            (
                appearance.appearance(),
                appearance
                    .last_error()
                    .or_else(|| appearance.load_diagnostic())
                    .map(str::to_owned),
            )
        };
        let active_model = self
            .state
            .models
            .iter()
            .find(|model| model.id == self.state.current_model);
        let reasoning_summary_available = active_model
            .and_then(|model| model.supports_reasoning_summary)
            .unwrap_or(false);
        let text_verbosity_available = active_model.is_some_and(|model| model.supports_verbosity);
        let can_open_model_picker = can_open_model_picker(&self.provider, self.state.can_send());
        let restart_note = if settings.restart_required {
            "Restart Snow to apply persisted runtime changes."
        } else {
            "Live values save immediately; persisted runtime values apply after restart."
        };
        v_flex()
            .track_focus(&self.settings_focus_handle)
            .min_w(px(0.))
            .min_h(px(0.))
            .flex_1()
            .h_full()
            .bg(theme.background)
            .child(
                h_flex()
                    .h(px(58.))
                    .w_full()
                    .flex_shrink_0()
                    .items_center()
                    .justify_between()
                    .gap_3()
                    .px_6()
                    .border_b_1()
                    .border_color(theme.border)
                    .child(
                        h_flex()
                            .min_w(px(0.))
                            .items_center()
                            .gap_3()
                            .child(
                                div()
                                    .text_sm()
                                    .text_color(theme.muted_foreground)
                                    .child("Settings"),
                            )
                            .child(
                                div()
                                    .text_sm()
                                    .text_color(theme.muted_foreground)
                                    .child("/"),
                            )
                            .child(
                                div()
                                    .text_sm()
                                    .font_semibold()
                                    .child(self.settings_section.label()),
                            ),
                    )
                    .child(
                        h_flex()
                            .gap_1()
                            .child(
                                Button::new("refresh-settings")
                                    .label("Refresh")
                                    .ghost()
                                    .on_click(cx.listener(|this, _, _, cx| {
                                        this.refresh_settings(cx)
                                    })),
                            )
                            .child(
                                Button::new("close-settings")
                                    .label("Done")
                                    .ghost()
                                    .on_click(cx.listener(|this, _, window, cx| {
                                        this.close_settings_panel(window, cx)
                                    })),
                            ),
                    ),
            )
            .child(
                v_flex()
                    .min_h(px(0.))
                    .flex_1()
                    .overflow_y_scrollbar()
                    .child(
                        v_flex()
                            .w_full()
                            .max_w(px(860.))
                            .mx_auto()
                            .gap_5()
                            .px_8()
                            .py_8()
                            .child(
                                v_flex()
                                    .gap_2()
                                    .child(
                                        div()
                                            .text_2xl()
                                            .font_semibold()
                                            .child(self.settings_section.label()),
                                    )
                                    .child(
                                        div()
                                            .text_sm()
                                            .text_color(theme.muted_foreground)
                                            .child(self.settings_section.description()),
                                    )
                                    .child(
                                        div()
                                            .pt_1()
                                            .text_xs()
                                            .text_color(if settings.restart_required {
                                                theme.warning
                                            } else {
                                                theme.muted_foreground
                                            })
                                            .child(restart_note),
                                    ),
                            )
                            .when(
                                self.settings_section == SettingsSection::General,
                                |settings_view| {
                                    settings_view.child(
                                        v_flex()
                            .gap_4()
                            .p_5()
                            .rounded_xl()
                            .border_1()
                            .border_color(theme.border)
                            .bg(theme.background)
                            .child(
                                h_flex()
                            .flex_wrap()
                            .gap_2()
                            .items_center()
                            .justify_between()
                            .child(
                                div()
                                    .text_sm()
                                    .font_medium()
                                    .child(format!("Model  {settings_provider}/{settings_model}")),
                            )
                            .child({
                                let workspace = cx.entity().downgrade();
                                Popover::new("settings-model-popover")
                                    .anchor(Corner::BottomRight)
                                    .open(
                                        self.composer_picker.active
                                            == Some(ComposerPicker::Model),
                                    )
                                    .on_open_change(move |open, window, cx| {
                                        let _ = workspace.update(cx, |this, cx| {
                                            this.set_composer_picker_open(
                                                ComposerPicker::Model,
                                                *open,
                                                window,
                                                cx,
                                            )
                                        });
                                    })
                                    .trigger(
                                        Button::new("settings-model")
                                            .label("Choose model…")
                                            .ghost()
                                            .disabled(!can_open_model_picker),
                                    )
                                    .child(self.render_model_picker(cx))
                            }),
                    )
                    .child(
                        v_flex()
                            .gap_1()
                            .child(div().text_sm().font_medium().child("Thinking effort"))
                            .child(h_flex().flex_wrap().gap_1().children(
                                self.state
                                    .thinking_levels
                                    .iter()
                                    .enumerate()
                                    .map(|(index, level)| {
                                        let target = level.clone();
                                        let selected = settings.thinking == *level;
                                        Button::new(("settings-thinking", index))
                                            .label(thinking_label(level))
                                            .when(selected, |button| button.primary())
                                            .when(!selected, |button| button.ghost())
                                            .on_click(cx.listener(move |this, _, window, cx| {
                                                this.select_thinking(&target, window, cx)
                                            }))
                                    }),
                            )),
                    )
                    .child(
                        v_flex()
                            .gap_1()
                            .child(
                                div().text_sm().font_medium().child(if reasoning_summary_available {
                                    "Reasoning summary"
                                } else {
                                    "Reasoning summary · unavailable for this model"
                                }),
                            )
                            .child(h_flex().flex_wrap().gap_1().children(
                                ["auto", "detailed", "none"].into_iter().enumerate().map(
                                    |(index, value)| {
                                        let selected = settings.reasoning_summary == value;
                                        Button::new(("settings-reasoning", index))
                                            .label(value)
                                            .when(selected, |button| button.primary())
                                            .when(!selected, |button| button.ghost())
                                            .disabled(!reasoning_summary_available)
                                            .on_click(cx.listener(move |this, _, _, cx| {
                                                this.set_live_setting(
                                                    "set_reasoning_summary",
                                                    serde_json::Map::from_iter([(
                                                        "reasoning_summary".into(),
                                                        serde_json::Value::String(value.into()),
                                                    )]),
                                                    cx,
                                                )
                                            }))
                                    },
                                ),
                            )),
                    )
                    .child(
                        v_flex()
                            .gap_1()
                            .child(
                                div().text_sm().font_medium().child(if text_verbosity_available {
                                    "Text verbosity"
                                } else {
                                    "Text verbosity · unavailable for this model"
                                }),
                            )
                            .child(h_flex().flex_wrap().gap_1().children(
                                ["low", "medium", "high"].into_iter().enumerate().map(
                                    |(index, value)| {
                                        let selected = settings.text_verbosity == value;
                                        Button::new(("settings-verbosity", index))
                                            .label(value)
                                            .when(selected, |button| button.primary())
                                            .when(!selected, |button| button.ghost())
                                            .disabled(!text_verbosity_available)
                                            .on_click(cx.listener(move |this, _, _, cx| {
                                                this.set_live_setting(
                                                    "set_text_verbosity",
                                                    serde_json::Map::from_iter([(
                                                        "text_verbosity".into(),
                                                        serde_json::Value::String(value.into()),
                                                    )]),
                                                    cx,
                                                )
                                            }))
                                    },
                                ),
                            )),
                    )
                    .child(
                        v_flex()
                            .gap_1()
                            .child(div().text_sm().font_medium().child("Session permission"))
                            .child(h_flex().flex_wrap().gap_1().children(
                                ["ask", "allow", "deny"].into_iter().enumerate().map(
                                    |(index, value)| {
                                        let selected = settings.permission_mode == value;
                                        Button::new(("settings-permission", index))
                                            .label(value)
                                            .when(selected, |button| button.primary())
                                            .when(!selected, |button| button.ghost())
                                            .on_click(cx.listener(move |this, _, _, cx| {
                                                this.set_live_setting(
                                                    "permission_mode_set",
                                                    serde_json::Map::from_iter([(
                                                        "params".into(),
                                                        serde_json::json!({"mode": value}),
                                                    )]),
                                                    cx,
                                                )
                                            }))
                                    },
                                ),
                            )),
                    )
                    )
                                },
                            )
                            .when(
                                self.settings_section == SettingsSection::Capabilities,
                                |settings_view| {
                                    settings_view.child(
                                        v_flex()
                            .gap_4()
                            .p_5()
                            .rounded_xl()
                            .border_1()
                            .border_color(theme.border)
                            .bg(theme.background)
                            .child(
                                h_flex()
                                    .flex_wrap()
                                    .gap_2()
                                    .items_center()
                                    .justify_between()
                                    .child(
                                        v_flex()
                                            .child(div().text_sm().font_medium().child("Subagents"))
                                    .child(
                                        div()
                                            .text_xs()
                                            .text_color(theme.muted_foreground)
                                            .child(format!(
                                                "{} · agent limit {}{}",
                                                if settings.subagents_enabled {
                                                    "enabled"
                                                } else {
                                                    "disabled"
                                                },
                                                settings.subagents_max_agents,
                                                if settings.subagents_restart_required {
                                                    " · restart required"
                                                } else {
                                                    ""
                                                }
                                            )),
                                    ),
                            )
                            .child(
                                Button::new("settings-subagents")
                                    .label(if settings.subagents_enabled {
                                        "Disable"
                                    } else {
                                        "Enable"
                                    })
                                    .on_click(cx.listener(|this, _, _, cx| {
                                        this.toggle_subagents_setting(cx)
                                    })),
                            ),
                    )
                    .child(
                        h_flex()
                            .flex_wrap()
                            .gap_2()
                            .items_center()
                            .child(
                                Input::new(&self.settings_concurrency_input)
                                    .w(px(220.)),
                            )
                            .child(
                                Button::new("settings-concurrency")
                                    .label("Save concurrency")
                                    .ghost()
                                    .on_click(cx.listener(|this, _, _, cx| {
                                        this.save_subagent_concurrency(cx)
                                    })),
                            ),
                    )
                    .child(
                        h_flex()
                            .flex_wrap()
                            .gap_2()
                            .items_center()
                            .justify_between()
                            .child(
                                v_flex()
                                    .child(div().text_sm().font_medium().child("Agent Skills"))
                                    .child(
                                        div()
                                            .text_xs()
                                            .text_color(theme.muted_foreground)
                                            .child(format!(
                                                "{}{}",
                                                if settings.skills_enabled {
                                                    "enabled"
                                                } else {
                                                    "disabled"
                                                },
                                                if settings.skills_restart_required {
                                                    " · restart required"
                                                } else {
                                                    ""
                                                }
                                            )),
                                    ),
                            )
                            .child(
                                Button::new("settings-skills")
                                    .label(if settings.skills_enabled {
                                        "Disable"
                                    } else {
                                        "Enable"
                                    })
                                    .on_click(cx.listener(|this, _, _, cx| {
                                        this.toggle_skills_setting(cx)
                                    })),
                            ),
                    )
                    .child(
                        h_flex()
                            .flex_wrap()
                            .gap_2()
                            .items_center()
                            .justify_between()
                            .child(div().text_sm().font_medium().child(format!(
                                "Debug diagnostics  {}",
                                if settings.debug_enabled { "enabled" } else { "disabled" }
                            )))
                            .child(
                                Button::new("settings-debug")
                                    .label(if settings.debug_enabled { "Disable" } else { "Enable" })
                                    .danger()
                                    .on_click(cx.listener(move |this, _, _, cx| {
                                        this.set_live_setting(
                                            if debug_enabled {
                                                "debug_disable"
                                            } else {
                                                "debug_enable"
                                            },
                                            serde_json::Map::new(),
                                            cx,
                                        )
                                    })),
                            ),
                    )
                    )
                                },
                            )
                            .when(
                                self.settings_section == SettingsSection::Appearance,
                                |settings_view| {
                                    settings_view.child(
                                        v_flex()
                            .gap_4()
                            .p_5()
                            .rounded_xl()
                            .border_1()
                            .border_color(theme.border)
                            .bg(theme.background)
                            .when_some(self.theme_catalog.as_ref(), |settings_view, catalog| {
                        settings_view.child(
                            v_flex()
                                .gap_2()
                                .child(div().text_sm().font_medium().child("Snow theme"))
                                .child(
                                    h_flex().flex_wrap().gap_1().children(
                                        catalog.themes.iter().enumerate().map(|(index, item)| {
                                            let name = item.name.clone();
                                            let selected = settings.theme == item.name
                                                || (settings.theme.is_empty()
                                                    && catalog.selected == item.name);
                                            Button::new(("settings-theme", index))
                                                .label(format!(
                                                    "{} · {}",
                                                    item.display_name, item.scope
                                                ))
                                                .when(selected, |button| button.primary())
                                                .when(!selected, |button| button.ghost())
                                                .on_click(cx.listener(move |this, _, _, cx| {
                                                    this.select_theme(&name, cx)
                                                }))
                                        }),
                                    ),
                                )
                                .child(
                                    div()
                                        .text_xs()
                                        .text_color(theme.muted_foreground)
                                        .child("Themes are resolved and persisted by Snow; native System/Light/Dark appearance remains desktop-owned."),
                                ),
                        )
                    })
                    .child(
                        v_flex()
                            .gap_2()
                            .child(div().text_sm().font_medium().child("Appearance"))
                            .child(
                                h_flex().flex_wrap().gap_2().children(
                                    [Appearance::System, Appearance::Light, Appearance::Dark]
                                        .into_iter()
                                        .enumerate()
                                        .map(|(index, appearance)| {
                                            Button::new(("settings-appearance", index))
                                                .label(appearance_label(appearance))
                                                .when(appearance == selected_appearance, |button| {
                                                    button.primary()
                                                })
                                                .when(appearance != selected_appearance, |button| {
                                                    button.ghost()
                                                })
                                                .on_click(cx.listener(
                                                    move |this, _, window, cx| {
                                                        this.set_desktop_appearance(
                                                            appearance, window, cx,
                                                        )
                                                    },
                                                ))
                                        }),
                                ),
                            )
                            .child(
                                div()
                                    .text_xs()
                                    .text_color(theme.muted_foreground)
                                    .child("System follows operating-system appearance; Light and Dark remain pinned."),
                            )
                            .when_some(appearance_diagnostic, |section, diagnostic| {
                                section.child(
                                    div()
                                        .text_xs()
                                        .text_color(theme.warning)
                                        .child(format!("Appearance preference: {diagnostic}")),
                                )
                            }),
                    )
                    )
                                },
                            )
                            .when(
                                self.settings_section == SettingsSection::Keybindings,
                                |settings_view| {
                                    settings_view.when_some(
                                        self.keybindings.as_ref(),
                                        |settings_view, bindings| {
                        settings_view.child(
                            v_flex()
                                .gap_3()
                                .p_5()
                                .rounded_xl()
                                .border_1()
                                .border_color(theme.border)
                                .bg(theme.background)
                                .child(
                                    h_flex()
                                        .flex_wrap()
                                        .gap_2()
                                        .justify_between()
                                        .child(div().text_sm().font_medium().child("Semantic keybindings"))
                                        .child(
                                            div()
                                                .text_xs()
                                                .text_color(theme.muted_foreground)
                                                .child(if bindings.project_allowed {
                                                    "Global and project layers"
                                                } else {
                                                    "Global layer · project untrusted"
                                                }),
                                        ),
                                )
                                .when_some(self.keybinding_edit_action.as_ref(), |section, action| {
                                    let global_selected =
                                        self.keybinding_edit_scope == KeybindingScope::Global;
                                    let project_selected =
                                        self.keybinding_edit_scope == KeybindingScope::Project;
                                    section.child(
                                        v_flex()
                                            .gap_2()
                                            .p_3()
                                            .rounded_lg()
                                            .border_1()
                                            .border_color(theme.border)
                                            .child(div().text_xs().font_semibold().child(format!(
                                                "Edit {}",
                                                action
                                            )))
                                            .child(
                                                h_flex()
                                                    .flex_wrap()
                                                    .gap_1()
                                                    .child(
                                                        Button::new("keybinding-scope-global")
                                                            .label("Global")
                                                            .when(global_selected, |button| button.primary())
                                                            .when(!global_selected, |button| button.ghost())
                                                            .on_click(cx.listener({
                                                                let action = action.clone();
                                                                move |this, _, window, cx| {
                                                                    this.edit_keybinding(
                                                                        &action,
                                                                        KeybindingScope::Global,
                                                                        window,
                                                                        cx,
                                                                    )
                                                                }
                                                            })),
                                                    )
                                                    .child(
                                                        Button::new("keybinding-scope-project")
                                                            .label("Project")
                                                            .disabled(!bindings.project_allowed)
                                                            .when(project_selected, |button| button.primary())
                                                            .when(!project_selected, |button| button.ghost())
                                                            .on_click(cx.listener({
                                                                let action = action.clone();
                                                                move |this, _, window, cx| {
                                                                    this.edit_keybinding(
                                                                        &action,
                                                                        KeybindingScope::Project,
                                                                        window,
                                                                        cx,
                                                                    )
                                                                }
                                                            })),
                                                    ),
                                            )
                                            .child(Input::new(&self.keybinding_input).w_full())
                                            .child(
                                                h_flex()
                                                    .flex_wrap()
                                                    .gap_1()
                                                    .child(
                                                        Button::new("keybinding-save")
                                                            .label("Save replacement")
                                                            .on_click(cx.listener(|this, _, _, cx| {
                                                                this.save_keybinding(cx)
                                                            })),
                                                    )
                                                    .child(
                                                        Button::new("keybinding-cancel")
                                                            .label("Cancel")
                                                            .ghost()
                                                            .on_click(cx.listener(|this, _, _, cx| {
                                                                this.close_keybinding_editor(cx)
                                                            })),
                                                    ),
                                            ),
                                    )
                                })
                                .child(
                                    v_flex()
                                        .max_h(px(260.))
                                        .overflow_y_scrollbar()
                                        .children(bindings.actions.iter().enumerate().map(
                                            |(index, item)| {
                                                let global_action = item.name.clone();
                                                let project_action = item.name.clone();
                                                let reset_global_action = item.name.clone();
                                                let reset_project_action = item.name.clone();
                                                v_flex()
                                                    .gap_1()
                                                    .py_2()
                                                    .border_t_1()
                                                    .border_color(theme.border)
                                                    .child(
                                                        h_flex()
                                                            .flex_wrap()
                                                            .gap_2()
                                                            .justify_between()
                                                            .child(
                                                                v_flex()
                                                                    .child(div().text_xs().font_semibold().child(item.name.clone()))
                                                                    .child(
                                                                        div()
                                                                            .text_xs()
                                                                            .text_color(theme.muted_foreground)
                                                                            .child(format!(
                                                                                "{} · source {}",
                                                                                if item.effective.is_empty() {
                                                                                    "unbound".into()
                                                                                } else {
                                                                                    item.effective.join(", ")
                                                                                },
                                                                                item.source
                                                                            )),
                                                                    ),
                                                            )
                                                            .child(
                                                                h_flex()
                                                                    .flex_wrap()
                                                                    .gap_1()
                                                                    .child(
                                                                        Button::new(("keybinding-edit-global", index))
                                                                            .label("Global")
                                                                            .ghost()
                                                                            .on_click(cx.listener(move |this, _, window, cx| {
                                                                                this.edit_keybinding(
                                                                                    &global_action,
                                                                                    KeybindingScope::Global,
                                                                                    window,
                                                                                    cx,
                                                                                )
                                                                            })),
                                                                    )
                                                                    .child(
                                                                        Button::new(("keybinding-reset-global", index))
                                                                            .label("Reset G")
                                                                            .ghost()
                                                                            .on_click(cx.listener(move |this, _, _, cx| {
                                                                                this.reset_keybinding(
                                                                                    &reset_global_action,
                                                                                    KeybindingScope::Global,
                                                                                    cx,
                                                                                )
                                                                            })),
                                                                    )
                                                                    .child(
                                                                        Button::new(("keybinding-edit-project", index))
                                                                            .label("Project")
                                                                            .ghost()
                                                                            .disabled(!bindings.project_allowed)
                                                                            .on_click(cx.listener(move |this, _, window, cx| {
                                                                                this.edit_keybinding(
                                                                                    &project_action,
                                                                                    KeybindingScope::Project,
                                                                                    window,
                                                                                    cx,
                                                                                )
                                                                            })),
                                                                    )
                                                                    .child(
                                                                        Button::new(("keybinding-reset-project", index))
                                                                            .label("Reset P")
                                                                            .ghost()
                                                                            .disabled(!bindings.project_allowed)
                                                                            .on_click(cx.listener(move |this, _, _, cx| {
                                                                                this.reset_keybinding(
                                                                                    &reset_project_action,
                                                                                    KeybindingScope::Project,
                                                                                    cx,
                                                                                )
                                                                            })),
                                                                    ),
                                                            ),
                                                    )
                                            },
                                        )),
                                ),
                        )
                    })
                                },
                            )
                            .when(
                                self.settings_section == SettingsSection::Keybindings,
                                |settings_view| {
                                    settings_view.child(
                                        div()
                                            .text_xs()
                                            .text_color(theme.muted_foreground)
                                            .child("Use the Help panel for keyboard shortcuts."),
                                    )
                                },
                            ),
                    ),
            )
    }

    fn render_auth_panel(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme();
        let selected = self.selected_auth_provider();
        let selected_method = selected.and_then(|provider| {
            self.auth_selected_method.as_deref().and_then(|method| {
                provider
                    .methods
                    .iter()
                    .find(|candidate| candidate.id == method)
            })
        });
        let login_running = self
            .auth_job
            .as_ref()
            .is_some_and(|job| job.state == "running");
        v_flex()
            .w_full()
            .max_h(px(520.))
            .flex_shrink_0()
            .border_b_1()
            .border_color(theme.border)
            .bg(theme.secondary)
            .child(
                h_flex()
                    .w_full()
                    .px_5()
                    .py_2()
                    .items_center()
                    .justify_between()
                    .child(
                        v_flex()
                            .child(div().font_semibold().child("Provider authentication"))
                            .child(
                                div()
                                    .text_xs()
                                    .text_color(theme.muted_foreground)
                                    .child("Credentials use Snow’s private auth store and are never displayed."),
                            ),
                    )
                    .child(
                        Button::new("close-auth")
                            .label("Close")
                            .ghost()
                            .on_click(cx.listener(|this, _, _, cx| this.close_auth_panel(cx))),
                    ),
            )
            .child(
                h_flex()
                    .min_h(px(0.))
                    .items_start()
                    .child(
                        v_flex()
                            .w(px(250.))
                            .max_h(px(420.))
                            .overflow_y_scrollbar()
                            .border_r_1()
                            .border_color(theme.border)
                            .p_2()
                            .children(
                                self.auth_providers
                                    .iter()
                                    .filter(|provider| {
                                        is_user_visible_provider(&provider.provider_id)
                                    })
                                    .enumerate()
                                    .map(|(index, provider)| {
                                    let provider_id = provider.provider_id.clone();
                                    let is_selected = self.auth_selected_provider.as_deref()
                                        == Some(provider.provider_id.as_str());
                                    Button::new(("auth-provider", index))
                                        .w_full()
                                        .label(format!(
                                            "{} · {}",
                                            if provider.display_name.is_empty() {
                                                &provider.provider_id
                                            } else {
                                                &provider.display_name
                                            },
                                            provider.status.state
                                        ))
                                        .when(is_selected, |button| button.primary())
                                        .when(!is_selected, |button| button.ghost())
                                        .on_click(cx.listener(move |this, _, _, cx| {
                                            this.select_auth_provider(&provider_id, cx)
                                        }))
                                    }),
                            ),
                    )
                    .child(
                        v_flex()
                            .min_w(px(0.))
                            .flex_1()
                            .max_h(px(420.))
                            .overflow_y_scrollbar()
                            .gap_3()
                            .p_4()
                            .when_some(selected, |panel, provider| {
                                panel
                                    .child(
                                        h_flex()
                                            .items_center()
                                            .justify_between()
                                            .child(
                                                v_flex()
                                                    .child(
                                                        div().font_medium().child(if provider
                                                            .display_name
                                                            .is_empty()
                                                        {
                                                            provider.provider_id.clone()
                                                        } else {
                                                            provider.display_name.clone()
                                                        }),
                                                    )
                                                    .child(
                                                        div()
                                                            .text_xs()
                                                            .text_color(theme.muted_foreground)
                                                            .child(format!(
                                                                "{}{}",
                                                                provider.status.state,
                                                                if provider.status.summary.is_empty()
                                                                {
                                                                    String::new()
                                                                } else {
                                                                    format!(
                                                                        " · {}",
                                                                        provider.status.summary
                                                                    )
                                                                }
                                                            )),
                                                    ),
                                            )
                                            .child(
                                                Button::new("auth-logout")
                                                    .label("Log out")
                                                    .danger()
                                                    .disabled(login_running)
                                                    .on_click(cx.listener(|this, _, _, cx| {
                                                        this.logout_selected_auth_provider(cx)
                                                    })),
                                            ),
                                    )
                                    .child(
                                        h_flex().flex_wrap().gap_1().children(
                                            provider.methods.iter().enumerate().map(
                                                |(index, method)| {
                                                    let method_id = method.id.clone();
                                                    let selected_method = self
                                                        .auth_selected_method
                                                        .as_deref()
                                                        == Some(method.id.as_str());
                                                    Button::new(("auth-method", index))
                                                        .label(if method.display_name.is_empty() {
                                                            method.id.clone()
                                                        } else {
                                                            method.display_name.clone()
                                                        })
                                                        .when(selected_method, |button| {
                                                            button.primary()
                                                        })
                                                        .when(!selected_method, |button| {
                                                            button.ghost()
                                                        })
                                                        .disabled(login_running)
                                                        .on_click(cx.listener(
                                                            move |this, _, _, cx| {
                                                                this.select_auth_method(
                                                                    &method_id, cx,
                                                                )
                                                            },
                                                        ))
                                                },
                                            ),
                                        ),
                                    )
                                    .when(
                                        provider.provider_id == "openai-compatible",
                                        |panel| {
                                            panel
                                                .child(
                                                    div()
                                                        .text_xs()
                                                        .text_color(theme.muted_foreground)
                                                        .child("Optional named OpenAI-compatible profile"),
                                                )
                                                .child(
                                                    Input::new(&self.auth_profile_id_input)
                                                        .w_full()
                                                        .disabled(login_running),
                                                )
                                                .child(
                                                    Input::new(&self.auth_base_url_input)
                                                        .w_full()
                                                        .disabled(login_running),
                                                )
                                        },
                                    )
                            })
                            .when(
                                selected_method.is_some_and(|method| method.kind == "api_key"),
                                |panel| {
                                    panel.child(
                                        Input::new(&self.auth_secret_input)
                                            .w_full()
                                            .mask_toggle()
                                            .disabled(login_running),
                                    )
                                },
                            )
                            .when_some(self.auth_job.as_ref(), |panel, job| {
                                panel.child(
                                    v_flex()
                                        .gap_2()
                                        .p_3()
                                        .rounded_lg()
                                        .bg(theme.background)
                                        .child(
                                            div()
                                                .text_sm()
                                                .font_medium()
                                                .child(format!(
                                                    "{} · {}",
                                                    job.state, job.method
                                                )),
                                        )
                                        .children(job.progress.iter().enumerate().map(
                                            |(index, progress)| {
                                                let url = progress.url.clone();
                                                let code = progress.user_code.clone();
                                                v_flex()
                                                    .gap_1()
                                                    .when(!progress.message.is_empty(), |row| {
                                                        row.child(
                                                            div()
                                                                .text_xs()
                                                                .child(progress.message.clone()),
                                                        )
                                                    })
                                                    .when(!url.is_empty(), |row| {
                                                        row.child(
                                                            h_flex()
                                                                .gap_2()
                                                                .items_center()
                                                                .child(
                                                                    div()
                                                                        .min_w(px(0.))
                                                                        .flex_1()
                                                                        .text_xs()
                                                                        .child(url.clone()),
                                                                )
                                                                .child(
                                                                    Button::new((
                                                                        "copy-auth-url",
                                                                        index,
                                                                    ))
                                                                    .label("Copy URL")
                                                                    .ghost()
                                                                    .on_click(cx.listener(
                                                                        move |_, _, _, cx| {
                                                                            cx.write_to_clipboard(
                                                                                ClipboardItem::new_string(
                                                                                    url.clone(),
                                                                                ),
                                                                            )
                                                                        },
                                                                    )),
                                                                ),
                                                        )
                                                    })
                                                    .when(!code.is_empty(), |row| {
                                                        row.child(
                                                            h_flex()
                                                                .gap_2()
                                                                .items_center()
                                                                .child(
                                                                    div()
                                                                        .text_sm()
                                                                        .font_semibold()
                                                                        .child(format!(
                                                                            "Device code: {code}"
                                                                        )),
                                                                )
                                                                .child(
                                                                    Button::new((
                                                                        "copy-auth-code",
                                                                        index,
                                                                    ))
                                                                    .label("Copy code")
                                                                    .ghost()
                                                                    .on_click(cx.listener(
                                                                        move |_, _, _, cx| {
                                                                            cx.write_to_clipboard(
                                                                                ClipboardItem::new_string(
                                                                                    code.clone(),
                                                                                ),
                                                                            )
                                                                        },
                                                                    )),
                                                                ),
                                                        )
                                                    })
                                            },
                                        ))
                                        .when(!job.error.is_empty(), |job_panel| {
                                            job_panel.child(
                                                div()
                                                    .text_xs()
                                                    .text_color(theme.danger)
                                                    .child(job.error.clone()),
                                            )
                                        }),
                                )
                            })
                            .child(
                                h_flex()
                                    .gap_2()
                                    .child(
                                        Button::new("auth-start")
                                            .label(if login_running {
                                                "Authenticating…"
                                            } else {
                                                "Authenticate"
                                            })
                                            .primary()
                                            .disabled(selected_method.is_none() || login_running)
                                            .on_click(cx.listener(|this, _, window, cx| {
                                                this.start_auth_login(window, cx)
                                            })),
                                    )
                                    .when(login_running, |actions| {
                                        actions.child(
                                            Button::new("auth-cancel")
                                                .label("Cancel")
                                                .danger()
                                                .on_click(cx.listener(|this, _, _, cx| {
                                                    this.cancel_auth_login(cx)
                                                })),
                                        )
                                    }),
                            ),
                    ),
            )
    }

    fn render_resource_panel(
        &self,
        panel: &ResourcePanel,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let theme = cx.theme();
        let copy_all = panel.resource.copy_text();
        v_flex()
            .w_full()
            .max_h(px(520.))
            .flex_shrink_0()
            .border_b_1()
            .border_color(theme.border)
            .bg(theme.secondary)
            .child(
                h_flex()
                    .w_full()
                    .px_5()
                    .py_2()
                    .items_center()
                    .justify_between()
                    .child(
                        v_flex()
                            .child(div().font_semibold().child(panel.resource.title.clone()))
                            .child(div().text_xs().text_color(theme.muted_foreground).child(
                                format!(
                                    "{} · {}{}",
                                    panel.command,
                                    panel.resource.summary,
                                    if panel.resource.truncated {
                                        " · additional rows omitted"
                                    } else {
                                        ""
                                    }
                                ),
                            )),
                    )
                    .child(
                        h_flex()
                            .gap_1()
                            .child(
                                Button::new("copy-resource-panel")
                                    .icon(IconName::Copy)
                                    .tooltip("Copy")
                                    .compact()
                                    .ghost()
                                    .on_click(move |_, _, cx| {
                                        cx.write_to_clipboard(ClipboardItem::new_string(
                                            copy_all.clone(),
                                        ))
                                    }),
                            )
                            .child(
                                Button::new("close-resource-panel")
                                    .label("Close")
                                    .ghost()
                                    .on_click(
                                        cx.listener(|this, _, _, cx| this.close_resource_panel(cx)),
                                    ),
                            ),
                    ),
            )
            .child(
                v_flex()
                    .mx_5()
                    .mb_3()
                    .max_h(px(410.))
                    .overflow_y_scrollbar()
                    .gap_3()
                    .children(panel.resource.sections.iter().enumerate().map(
                        |(section_index, section)| {
                            v_flex()
                                .gap_1()
                                .child(
                                    div()
                                        .text_xs()
                                        .font_semibold()
                                        .child(section.heading.clone()),
                                )
                                .children(section.rows.iter().enumerate().map(
                                    |(row_index, row)| {
                                        let tone = match row.tone {
                                            SemanticTone::Neutral => theme.foreground,
                                            SemanticTone::Positive => theme.success,
                                            SemanticTone::Caution => theme.warning,
                                            SemanticTone::Negative => theme.danger,
                                        };
                                        let copy = row.copy_text();
                                        v_flex()
                                            .gap_1()
                                            .p_3()
                                            .rounded_lg()
                                            .border_1()
                                            .border_color(theme.border)
                                            .bg(theme.background)
                                            .child(
                                                h_flex()
                                                    .justify_between()
                                                    .child(
                                                        div()
                                                            .text_xs()
                                                            .font_semibold()
                                                            .text_color(tone)
                                                            .child(row.label.clone()),
                                                    )
                                                    .child(
                                                        Button::new((
                                                            "copy-resource-row",
                                                            section_index * 1000 + row_index,
                                                        ))
                                                        .icon(IconName::Copy)
                                                        .tooltip("Copy")
                                                        .compact()
                                                        .ghost()
                                                        .on_click(move |_, _, cx| {
                                                            cx.write_to_clipboard(
                                                                ClipboardItem::new_string(
                                                                    copy.clone(),
                                                                ),
                                                            )
                                                        }),
                                                    ),
                                            )
                                            .child(div().text_sm().child(row.value.clone()))
                                            .when_some(row.detail.as_ref(), |card, detail| {
                                                card.child(
                                                    div()
                                                        .text_xs()
                                                        .text_color(theme.muted_foreground)
                                                        .child(detail.clone()),
                                                )
                                            })
                                    },
                                ))
                        },
                    )),
            )
    }

    fn render_subagents_panel(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme();
        let summary = self.subagent_fleet.summary();
        let summary_text = format!(
            "{} running · {} queued · {} terminal · {} open · {} closed · concurrency {} · agent limit {}{}",
            summary.running,
            summary.queued,
            summary.terminal,
            summary.open,
            summary.closed,
            summary.concurrent_limit,
            summary.agent_limit,
            if summary.truncated {
                " · truncated"
            } else {
                ""
            }
        );
        let selected = self.subagent_fleet.selected();
        let detail = self.subagent_fleet.detail().or(selected);
        let selected_thread = selected.map(|agent| agent.state.agent.thread_id.as_str());
        let activity = selected_thread.and_then(|thread| self.subagent_fleet.activity(thread));

        v_flex()
            .w_full()
            .max_h(px(620.))
            .flex_shrink_0()
            .border_b_1()
            .border_color(theme.border)
            .bg(theme.secondary)
            .child(
                h_flex()
                    .w_full()
                    .px_5()
                    .py_2()
                    .items_center()
                    .justify_between()
                    .child(
                        v_flex()
                            .child(div().font_semibold().child("Subagents"))
                            .child(
                                div()
                                    .text_xs()
                                    .text_color(theme.muted_foreground)
                                    .child(summary_text),
                            ),
                    )
                    .child(
                        h_flex()
                            .gap_1()
                            .child(
                                Button::new("refresh-subagents")
                                    .label("Refresh")
                                    .ghost()
                                    .on_click(
                                        cx.listener(|this, _, _, cx| this.refresh_subagents(cx)),
                                    ),
                            )
                            .child(
                                Button::new("close-subagents")
                                    .label("Close")
                                    .ghost()
                                    .on_click(cx.listener(|this, _, _, cx| {
                                        this.close_subagents_panel(cx)
                                    })),
                            ),
                    ),
            )
            .child(
                v_flex()
                    .min_h(px(0.))
                    .max_h(px(230.))
                    .overflow_y_scrollbar()
                    .children(self.subagent_fleet.agents().iter().enumerate().map(
                        |(index, versioned)| {
                            let agent = &versioned.state;
                            let (agent_provider, agent_model) =
                                presented_provider_model(&agent.provider, &agent.model);
                            let target = agent.agent.path.clone();
                            let selected = self
                                .subagent_fleet
                                .selection()
                                .is_some_and(|selection| {
                                    selection.path == agent.agent.path
                                        && selection.thread_id == agent.agent.thread_id
                                });
                            let action = selected
                                .then(|| self.subagent_fleet.selected_action())
                                .flatten();
                            let action_target = target.clone();
                            let action_label = action.map(|action| match action {
                                crate::subagent_live::SubagentAction::Interrupt => "Interrupt",
                                crate::subagent_live::SubagentAction::Resume => "Resume",
                                crate::subagent_live::SubagentAction::Close => "Close",
                            });
                            let action_button = action.zip(action_label).map(|(action, label)| {
                                Button::new(("subagent-action", index))
                                    .label(label)
                                    .ghost()
                                    .on_click(cx.listener(move |this, _, _, cx| {
                                        this.subagent_action(action, &action_target, cx)
                                    }))
                            });
                            v_flex()
                                .w_full()
                                .gap_1()
                                .px_5()
                                .py_3()
                                .border_t_1()
                                .border_color(theme.border)
                                .bg(if selected { theme.background } else { theme.secondary })
                                .child(
                                    h_flex()
                                        .items_start()
                                        .justify_between()
                                        .gap_3()
                                        .child(
                                            v_flex()
                                                .min_w(px(0.))
                                                .flex_1()
                                                .child(div().font_medium().child(if agent
                                                    .agent
                                                    .nickname
                                                    .is_empty()
                                                {
                                                    agent.agent.path.clone()
                                                } else {
                                                    format!(
                                                        "{} · {}",
                                                        agent.agent.nickname, agent.agent.path
                                                    )
                                                }))
                                                .child(
                                                    div()
                                                        .text_xs()
                                                        .text_color(theme.muted_foreground)
                                                        .child(format!(
                                                            "{} · {} · role {} · generation {}",
                                                            display_value(&agent.status),
                                                            agent.agent.thread_id,
                                                            display_value(&agent.agent.role),
                                                            versioned.generation
                                                        )),
                                                ),
                                        )
                                        .child(
                                            h_flex()
                                                .gap_1()
                                                .child(
                                                    Button::new(("select-subagent", index))
                                                        .label(if selected { "Selected" } else { "Details" })
                                                        .ghost()
                                                        .on_click(cx.listener(move |this, _, _, cx| {
                                                            this.select_subagent(&target, cx)
                                                        })),
                                                )
                                                .when_some(action_button, |actions, button| {
                                                    actions.child(button)
                                                }),
                                        ),
                                )
                                .child(
                                    div()
                                        .text_xs()
                                        .text_color(theme.muted_foreground)
                                        .child(format!(
                                            "provider {} · model {} · thinking {} · started {} · finished {}",
                                            display_value(agent_provider),
                                            display_value(agent_model),
                                            display_value(&agent.thinking),
                                            agent.started_at,
                                            agent.finished_at
                                        )),
                                )
                        },
                    )),
            )
            .when_some(detail, |panel, versioned| {
                let agent = &versioned.state;
                let (agent_provider, agent_model) =
                    presented_provider_model(&agent.provider, &agent.model);
                panel.child(
                    v_flex()
                        .gap_1()
                        .mx_5()
                        .my_2()
                        .p_3()
                        .rounded_lg()
                        .border_1()
                        .border_color(theme.border)
                        .bg(theme.background)
                        .child(div().text_xs().font_semibold().child("Selected agent detail"))
                        .child(
                            div()
                                .text_xs()
                                .text_color(theme.muted_foreground)
                                .child(format!(
                                    "{} · parent {} · depth {} · created {} · started {} · finished {}",
                                    agent.agent.path,
                                    display_value(&agent.agent.parent_path),
                                    agent.agent.depth,
                                    agent.created_at,
                                    agent.started_at,
                                    agent.finished_at
                                )),
                        )
                        .child(
                            div()
                                .text_xs()
                                .text_color(theme.muted_foreground)
                                .child(format!(
                                    "status {} · provider {} · model {} · thinking {}",
                                    display_value(&agent.status),
                                    display_value(agent_provider),
                                    display_value(agent_model),
                                    display_value(&agent.thinking)
                                )),
                        ),
                )
            })
            .when_some(activity, |panel, activity| {
                panel.child(
                    v_flex()
                        .gap_1()
                        .mx_5()
                        .mb_2()
                        .child(div().text_xs().font_semibold().child("Live activity"))
                        .children(activity.lines().iter().map(|line| {
                            div()
                                .text_xs()
                                .text_color(theme.muted_foreground)
                                .child(format!("{:?} · {}", line.kind, line.text))
                        })),
                )
            })
            .when(!self.subagent_transcript.messages.is_empty(), |panel| {
                panel.child(
                    v_flex()
                        .gap_2()
                        .mx_5()
                        .mb_3()
                        .max_h(px(280.))
                        .overflow_y_scrollbar()
                        .child(
                            div().text_xs().font_semibold().child(format!(
                                "Durable history · {} of {}{}",
                                self.subagent_transcript.messages.len(),
                                self.subagent_transcript.total,
                                if self.subagent_transcript.complete {
                                    ""
                                } else {
                                    " · loading"
                                }
                            )),
                        )
                        .children(
                            self.subagent_transcript
                                .messages
                                .iter()
                                .enumerate()
                                .map(|(index, message)| {
                                    self.render_subagent_history_message(message, index, theme)
                                }),
                        ),
                )
            })
    }

    fn render_subagent_history_message(
        &self,
        message: &SubagentHistoryMessage,
        index: usize,
        theme: &gpui_component::theme::Theme,
    ) -> AnyElement {
        let blocks = message
            .blocks
            .iter()
            .enumerate()
            .map(|(_block_index, block)| match block {
                HistoryBlock::Text { text } => {
                    div().text_sm().child(text.clone()).into_any_element()
                }
                HistoryBlock::Plan { text, complete } => v_flex()
                    .gap_1()
                    .p_2()
                    .rounded_md()
                    .border_1()
                    .border_color(if *complete { theme.success } else { theme.info })
                    .child(div().text_xs().font_semibold().child(if *complete {
                        "Plan · complete"
                    } else {
                        "Plan · in progress"
                    }))
                    .child(div().text_sm().child(text.clone()))
                    .into_any_element(),
                HistoryBlock::Image(image) => v_flex()
                    .gap_1()
                    .child(
                        img(Arc::clone(&image.preview))
                            .max_w(px(360.))
                            .max_h(px(240.))
                            .rounded_md(),
                    )
                    .child(
                        div()
                            .text_xs()
                            .text_color(theme.muted_foreground)
                            .child(format!(
                                "{} · {} KiB",
                                image.mime_type,
                                image.data.len().div_ceil(1024)
                            )),
                    )
                    .into_any_element(),
                HistoryBlock::ToolCall(tool) => v_flex()
                    .gap_1()
                    .p_2()
                    .rounded_md()
                    .bg(theme.secondary)
                    .child(
                        div()
                            .text_xs()
                            .font_semibold()
                            .child(format!("Tool · {}", tool.name)),
                    )
                    .when(!tool.arguments_display.is_empty(), |card| {
                        card.child(
                            div()
                                .text_xs()
                                .font_family("monospace")
                                .child(tool.arguments_display.clone()),
                        )
                    })
                    .into_any_element(),
            });
        v_flex()
            .gap_2()
            .p_3()
            .rounded_lg()
            .border_1()
            .border_color(theme.border)
            .bg(theme.background)
            .child(
                h_flex()
                    .justify_between()
                    .child(div().text_xs().font_semibold().child(format!(
                        "{} · {}",
                        display_value(&message.role),
                        message.timestamp
                    )))
                    .child(
                        div()
                            .text_xs()
                            .text_color(theme.muted_foreground)
                            .child(format!("message {}", index + 1)),
                    ),
            )
            .children(blocks)
            .when_some(message.tool_result.as_ref(), |card, result| {
                card.child(
                    v_flex()
                        .gap_1()
                        .p_2()
                        .rounded_md()
                        .border_1()
                        .border_color(if result.is_error {
                            theme.danger
                        } else {
                            theme.border
                        })
                        .child(
                            div()
                                .text_xs()
                                .font_semibold()
                                .child(format!("Tool result · {}", result.tool_name)),
                        )
                        .when(!result.display.output.is_empty(), |result_card| {
                            result_card.child(
                                div()
                                    .text_xs()
                                    .font_family("monospace")
                                    .child(result.display.output.clone()),
                            )
                        }),
                )
            })
            .into_any_element()
    }

    fn render_processes_panel(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme();
        let header = h_flex()
            .w_full()
            .px_5()
            .py_2()
            .items_center()
            .justify_between()
            .child(div().font_semibold().child("Managed processes"))
            .child(
                h_flex()
                    .gap_1()
                    .when(self.process_detail_open, |actions| {
                        actions.child(
                            Button::new("back-processes")
                                .label("Back")
                                .ghost()
                                .on_click(
                                    cx.listener(|this, _, _, cx| this.close_process_logs(cx)),
                                ),
                        )
                    })
                    .child(
                        Button::new("refresh-processes")
                            .label("Refresh")
                            .ghost()
                            .on_click(cx.listener(|this, _, _, cx| this.refresh_processes(cx))),
                    )
                    .child(
                        Button::new("close-processes")
                            .label("Close")
                            .ghost()
                            .on_click(cx.listener(|this, _, _, cx| this.close_processes_panel(cx))),
                    ),
            );
        let content =
            if self.process_detail_open
                && let Some(logs) = self.process_live.selected_process()
            {
                let title = if logs.name.is_empty() {
                    logs.process_id.as_str()
                } else {
                    logs.name.as_str()
                };
                let output = self.process_live.output().to_owned();
                v_flex()
                    .min_h(px(0.))
                    .gap_2()
                    .px_5()
                    .pb_3()
                    .child(
                        v_flex()
                            .gap_1()
                            .p_3()
                            .rounded_lg()
                            .bg(theme.background)
                            .child(
                                h_flex()
                                    .justify_between()
                                    .child(
                                        v_flex()
                                            .child(div().font_medium().child(title.to_owned()))
                                            .child(
                                                div()
                                                    .text_xs()
                                                    .text_color(theme.muted_foreground)
                                                    .child(format!(
                                                        "Selected process · {} · status {}",
                                                        logs.process_id,
                                                        display_value(&logs.status)
                                                    )),
                                            ),
                                    )
                                    .when(!self.process_live.output().is_empty(), |row| {
                                        row.child(
                                            Button::new("copy-process-logs")
                                                .label("Copy logs")
                                                .ghost()
                                                .on_click(move |_, _, cx| {
                                                    cx.write_to_clipboard(
                                                        ClipboardItem::new_string(output.clone()),
                                                    )
                                                }),
                                        )
                                    }),
                            )
                            .child(div().text_xs().text_color(theme.muted_foreground).child(
                                format!(
                                "ready {} · started {} · finished {} · exit {} · signal {}",
                                if logs.ready { "yes" } else { "no" },
                                logs.started_at,
                                logs.finished_at,
                                logs.exit_code
                                    .map(|code| code.to_string())
                                    .unwrap_or_else(|| "—".into()),
                                display_value(&logs.signal)
                            ),
                            ))
                            .child(
                                div()
                                    .text_xs()
                                    .text_color(theme.muted_foreground)
                                    .child(format!("reason {}", display_value(&logs.reason))),
                            )
                            .child(div().text_xs().text_color(theme.muted_foreground).child(
                                format!(
                                    "cursor {} · {} bytes omitted · eof {}",
                                    self.process_live.cursor(),
                                    self.process_live.output().len(),
                                    if self.process_live.terminal_eof() {
                                        "yes"
                                    } else {
                                        "no"
                                    }
                                ),
                            )),
                    )
                    .child(
                        div()
                            .max_h(px(240.))
                            .overflow_y_scrollbar()
                            .p_3()
                            .rounded_lg()
                            .bg(theme.background)
                            .font_family("monospace")
                            .text_xs()
                            .child(if self.process_live.output().is_empty() {
                                "No retained output.".into()
                            } else {
                                self.process_live.output().to_owned()
                            }),
                    )
                    .when(!self.process_live.terminal_eof(), |panel| {
                        panel.child(
                            Button::new("more-process-logs")
                                .label("Load more")
                                .ghost()
                                .on_click(
                                    cx.listener(|this, _, _, cx| this.load_more_process_logs(cx)),
                                ),
                        )
                    })
                    .into_any_element()
            } else {
                v_flex()
                    .min_h(px(0.))
                    .overflow_y_scrollbar()
                    .children(self.process_live.processes().iter().enumerate().map(
                        |(index, process)| {
                            let process_id = process.process_id.clone();
                            v_flex()
                                .w_full()
                                .gap_1()
                                .px_5()
                                .py_3()
                                .border_t_1()
                                .border_color(theme.border)
                                .child(
                                    h_flex()
                                        .w_full()
                                        .gap_3()
                                        .items_start()
                                        .justify_between()
                                        .child(
                                            v_flex()
                                                .min_w(px(0.))
                                                .flex_1()
                                                .child(div().font_medium().child(
                                                    if process.name.is_empty() {
                                                        process.process_id.clone()
                                                    } else {
                                                        process.name.clone()
                                                    },
                                                ))
                                                .child(
                                                    div()
                                                        .text_xs()
                                                        .text_color(theme.muted_foreground)
                                                        .child(format!(
                                                            "{} · {} · ready {}",
                                                            display_value(&process.status),
                                                            process.process_id,
                                                            if process.ready {
                                                                "yes"
                                                            } else {
                                                                "no"
                                                            }
                                                        )),
                                                ),
                                        )
                                        .child(
                                            Button::new(("process-logs", index))
                                                .label("Select details & logs")
                                                .ghost()
                                                .on_click(cx.listener(move |this, _, _, cx| {
                                                    this.open_process_logs(&process_id, cx)
                                                })),
                                        ),
                                )
                                .child(div().text_xs().text_color(theme.muted_foreground).child(
                                    format!(
                            "started {} · finished {} · exit {} · signal {} · reason {}",
                            process.started_at,
                            process.finished_at,
                            process
                                .exit_code
                                .map(|code| code.to_string())
                                .unwrap_or_else(|| "—".into()),
                            display_value(&process.signal),
                            display_value(&process.reason)
                        ),
                                ))
                        },
                    ))
                    .into_any_element()
            };
        v_flex()
            .w_full()
            .max_h(px(500.))
            .flex_shrink_0()
            .border_b_1()
            .border_color(theme.border)
            .bg(theme.secondary)
            .child(header)
            .child(content)
    }

    fn render_sessions_panel(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme();
        let can_mutate = self.state.can_send();
        let workspace = cx.entity().downgrade();
        let scroll_handle = self.management_session_list.clone();
        let session_rows = uniform_list(
            "management-session-list",
            self.sessions.len(),
            move |visible_range, _window, cx| {
                workspace
                    .update(cx, |this, cx| {
                        let theme = cx.theme();
                        visible_range
                            .filter_map(|index| {
                                let session = this.sessions.get(index)?;
                                let session_id = session.session_id.clone();
                                let open_id = session_id.clone();
                                let delete_id = session_id.clone();
                                let confirming_delete = this.session_delete_confirm.as_deref()
                                    == Some(session_id.as_str());
                                let title = if session.name.is_empty() {
                                    format!(
                                        "Session {}",
                                        &session.session_id[..session.session_id.len().min(8)]
                                    )
                                } else {
                                    session.name.clone()
                                };
                                Some(
                                    h_flex()
                                        .w_full()
                                        .h(px(MANAGEMENT_SESSION_ROW_HEIGHT))
                                        .overflow_hidden()
                                        .px_5()
                                        .py_2()
                                        .gap_3()
                                        .items_center()
                                        .border_t_1()
                                        .border_color(theme.border)
                                        .child(
                                            v_flex()
                                                .min_w(px(0.))
                                                .flex_1()
                                                .child(
                                                    div()
                                                        .w_full()
                                                        .overflow_hidden()
                                                        .text_ellipsis()
                                                        .whitespace_nowrap()
                                                        .font_medium()
                                                        .child(if session.active {
                                                            format!("{title} · Active")
                                                        } else {
                                                            title
                                                        }),
                                                )
                                                .child(
                                                    div()
                                                        .w_full()
                                                        .overflow_hidden()
                                                        .text_ellipsis()
                                                        .whitespace_nowrap()
                                                        .text_xs()
                                                        .text_color(theme.muted_foreground)
                                                        .child(format!(
                                                            "{} messages · updated {} · {}",
                                                            session.messages,
                                                            session.updated_at,
                                                            session.session_id
                                                        )),
                                                ),
                                        )
                                        .child(
                                            Button::new(domain_element_id(
                                                "open-session",
                                                &session_id,
                                            ))
                                            .label(if session.active { "Open" } else { "Switch" })
                                            .ghost()
                                            .disabled(!can_mutate || session.active)
                                            .on_click(cx.listener(move |this, _, window, cx| {
                                                this.open_session(&open_id, window, cx)
                                            })),
                                        )
                                        .child(
                                            Button::new(domain_element_id(
                                                "delete-session",
                                                &session_id,
                                            ))
                                            .label(if confirming_delete {
                                                "Confirm delete"
                                            } else {
                                                "Delete"
                                            })
                                            .danger()
                                            .disabled(!can_mutate || session.active)
                                            .on_click(cx.listener(move |this, _, _, cx| {
                                                this.request_session_delete(&delete_id, cx)
                                            })),
                                        )
                                        .when(confirming_delete, |row| {
                                            row.child(
                                                Button::new(domain_element_id(
                                                    "cancel-delete-session",
                                                    &session_id,
                                                ))
                                                .label("Cancel")
                                                .ghost()
                                                .on_click(cx.listener(|this, _, _, cx| {
                                                    this.cancel_session_delete(cx)
                                                })),
                                            )
                                        })
                                        .into_any_element(),
                                )
                            })
                            .collect::<Vec<_>>()
                    })
                    .unwrap_or_default()
            },
        )
        .track_scroll(scroll_handle.clone())
        .w_full()
        .h_full();
        let session_rows_height =
            (self.sessions.len() as f32 * MANAGEMENT_SESSION_ROW_HEIGHT).min(306.);
        v_flex()
            .w_full()
            .max_h(px(360.))
            .flex_shrink_0()
            .border_b_1()
            .border_color(theme.border)
            .bg(theme.secondary)
            .child(
                h_flex()
                    .w_full()
                    .px_5()
                    .py_2()
                    .items_center()
                    .justify_between()
                    .child(div().font_semibold().child("Project sessions"))
                    .child(
                        h_flex()
                            .gap_1()
                            .child(
                                Button::new("new-session")
                                    .label("New")
                                    .primary()
                                    .disabled(!can_mutate)
                                    .on_click(cx.listener(|this, _, window, cx| {
                                        this.create_session(window, cx)
                                    })),
                            )
                            .child(
                                Button::new("close-sessions")
                                    .label("Close")
                                    .ghost()
                                    .on_click(
                                        cx.listener(|this, _, _, cx| this.close_sessions_panel(cx)),
                                    ),
                            ),
                    ),
            )
            .child(
                div()
                    .w_full()
                    .h(px(session_rows_height))
                    .min_h(px(0.))
                    .overflow_hidden()
                    .child(session_rows)
                    .vertical_scrollbar(&scroll_handle),
            )
    }

    fn render_session_menu(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme();
        let can_manage = self.state.can_manage_session();
        let can_rename = can_manage
            && !self.session_name_input.read(cx).value().trim().is_empty()
            && self.session_name_input.read(cx).value().trim() != self.state.session_name;
        let branches = ordered_branch_topology(&self.state.branches);

        v_flex()
            .w(px(620.))
            .gap_4()
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
                                    .on_click(
                                        cx.listener(|this, _, _, cx| this.rename_session(cx)),
                                    ),
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
                    .children(branches.into_iter().map(|(branch, depth)| {
                        let branch_id = branch.id.clone();
                        let open_id = branch_id.clone();
                        let rename_id = branch_id.clone();
                        let delete_id = branch_id.clone();
                        let branch_name = if branch.name.is_empty() {
                            branch.id.clone()
                        } else {
                            branch.name.clone()
                        };
                        let rename_name = branch_name.clone();
                        let is_editing =
                            self.branch_editing_id.as_deref() == Some(branch_id.as_str());
                        let confirming_delete =
                            self.branch_delete_confirm.as_deref() == Some(branch_id.as_str());
                        v_flex()
                            .w_full()
                            .gap_1()
                            .child(
                                h_flex()
                                    .w_full()
                                    .min_h(px(38.))
                                    .gap_3()
                                    .px_2()
                                    .pl(px(8. + depth as f32 * 20.))
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
                                                    .child(format!(
                                                        "{}{}{}",
                                                        if depth == 0 { "● " } else { "↳ " },
                                                        branch_name,
                                                        if branch.active {
                                                            "  · ACTIVE"
                                                        } else {
                                                            ""
                                                        }
                                                    )),
                                            )
                                            .child(
                                                div()
                                                    .text_xs()
                                                    .text_color(theme.muted_foreground)
                                                    .child(format!(
                                                        "#{} · parent {} · {} messages",
                                                        short_id(&branch.id),
                                                        if branch.parent_branch_id.is_empty() {
                                                            "root"
                                                        } else {
                                                            short_id(&branch.parent_branch_id)
                                                        },
                                                        branch.messages
                                                    )),
                                            )
                                            .when(!branch.preview.is_empty(), |details| {
                                                details.child(
                                                    div()
                                                        .max_w(px(320.))
                                                        .overflow_hidden()
                                                        .text_ellipsis()
                                                        .text_xs()
                                                        .text_color(theme.muted_foreground)
                                                        .child(bounded_display(
                                                            &branch.preview,
                                                            160,
                                                        )),
                                                )
                                            }),
                                    )
                                    .child(
                                        h_flex()
                                            .gap_1()
                                            .child(
                                                Button::new(domain_element_id(
                                                    "select-branch",
                                                    &branch_id,
                                                ))
                                                .label(if branch.active {
                                                    "Current"
                                                } else {
                                                    "Open"
                                                })
                                                .ghost()
                                                .disabled(!can_manage || branch.active)
                                                .on_click(cx.listener(move |this, _, _, cx| {
                                                    this.select_branch(&open_id, cx)
                                                })),
                                            )
                                            .child(
                                                Button::new(domain_element_id(
                                                    "rename-branch",
                                                    &branch_id,
                                                ))
                                                .label("Rename")
                                                .ghost()
                                                .disabled(!can_manage)
                                                .on_click(cx.listener(
                                                    move |this, _, window, cx| {
                                                        this.begin_branch_rename(
                                                            &rename_id,
                                                            &rename_name,
                                                            window,
                                                            cx,
                                                        )
                                                    },
                                                )),
                                            )
                                            .child(
                                                Button::new(domain_element_id(
                                                    "delete-branch",
                                                    &branch_id,
                                                ))
                                                .label(if confirming_delete {
                                                    "Confirm delete"
                                                } else {
                                                    "Delete"
                                                })
                                                .ghost()
                                                .disabled(!can_manage || branch.active)
                                                .on_click(cx.listener(move |this, _, _, cx| {
                                                    this.request_branch_delete(&delete_id, cx)
                                                })),
                                            ),
                                    ),
                            )
                            .when(is_editing, |branch_row| {
                                branch_row.child(
                                    h_flex()
                                        .gap_2()
                                        .px_2()
                                        .child(
                                            div()
                                                .min_w(px(0.))
                                                .flex_1()
                                                .child(Input::new(&self.branch_name_input)),
                                        )
                                        .child(
                                            Button::new(domain_element_id(
                                                "save-branch",
                                                &branch_id,
                                            ))
                                            .label("Save")
                                            .primary()
                                            .on_click(cx.listener(|this, _, _, cx| {
                                                this.confirm_branch_rename(cx)
                                            })),
                                        )
                                        .child(
                                            Button::new(domain_element_id(
                                                "cancel-branch",
                                                &branch_id,
                                            ))
                                            .label("Cancel")
                                            .ghost()
                                            .on_click(cx.listener(|this, _, _, cx| {
                                                this.cancel_branch_rename(cx)
                                            })),
                                        ),
                                )
                            })
                            .when(confirming_delete, |branch_row| {
                                branch_row.child(
                                    h_flex()
                                        .px_2()
                                        .gap_2()
                                        .text_xs()
                                        .text_color(theme.danger)
                                        .child("Only leaf branches can be deleted.")
                                        .child(
                                            Button::new(domain_element_id(
                                                "cancel-delete-branch",
                                                &branch_id,
                                            ))
                                            .label("Cancel")
                                            .ghost()
                                            .on_click(cx.listener(|this, _, _, cx| {
                                                this.cancel_branch_delete(cx)
                                            })),
                                        ),
                                )
                            })
                    })),
            )
    }

    fn render_transcript(&self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let message_count = self.state.messages.len();
        sync_transcript_list_items(&self.transcript_list, message_count, false);

        let workspace = cx.entity().downgrade();
        let transcript_list = self.transcript_list.clone();
        let transcript = list(transcript_list.clone(), move |index, window, cx| {
            workspace
                .update(cx, |this, cx| {
                    let Some(message) = this.state.messages.get(index) else {
                        return div().into_any_element();
                    };
                    let tool_run = tool_activity_run_bounds(&this.state.messages, index);
                    if tool_run.is_some_and(|(_, end)| end != index) {
                        return div().h(px(0.)).into_any_element();
                    }
                    let coalesced = tool_run.and_then(|(start, end)| {
                        coalesced_tool_activity_message(&this.state.messages, start, end)
                    });
                    let compact = coalesced.is_some();
                    let message = coalesced.as_ref().unwrap_or(message);
                    let theme = cx.theme().clone();
                    div()
                        .w_full()
                        .overflow_hidden()
                        .flex()
                        .justify_center()
                        .child(
                            div()
                                .w_full()
                                .min_w(px(0.))
                                .max_w(px(CONVERSATION_WIDTH))
                                .overflow_hidden()
                                .px_6()
                                .when(compact, |row| row.pb_1())
                                .when(!compact, |row| row.pb_6())
                                .child(this.render_message(index, message, &theme, window, cx)),
                        )
                        .into_any_element()
                })
                .unwrap_or_else(|_| div().into_any_element())
        })
        .w_full()
        .h_full()
        .pt_8();

        div()
            .id("transcript")
            .key_context("DesktopTranscript")
            .relative()
            .min_h(px(0.))
            .overflow_hidden()
            .flex_1()
            .child(transcript)
            .vertical_scrollbar(&transcript_list)
    }

    fn render_empty_state(&self, theme: &gpui_component::theme::Theme) -> impl IntoElement {
        v_flex()
            .max_w(px(620.))
            .items_center()
            .gap_3()
            .child(
                div()
                    .text_2xl()
                    .font_semibold()
                    .text_center()
                    .child(format!("What should we build in {}?", self.project_label())),
            )
            .child(
                div()
                    .text_center()
                    .text_sm()
                    .text_color(theme.muted_foreground)
                    .child("Start a Snow thread with a task, question, or plan."),
            )
    }

    fn render_history_tool_card(
        &self,
        tool: Option<&HistoryToolCall>,
        result: Option<&HistoryToolResult>,
        card_id: u64,
        message_index: usize,
        theme: &gpui_component::theme::Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let name = tool
            .map(|tool| tool.name.as_str())
            .filter(|name| !name.is_empty())
            .or_else(|| {
                result
                    .map(|result| result.tool_name.as_str())
                    .filter(|name| !name.is_empty())
            })
            .unwrap_or("Tool");
        let raw_arguments = tool
            .map(|tool| tool.arguments_display.trim())
            .filter(|arguments| !arguments.is_empty());
        let running_detail = raw_arguments
            .and_then(|arguments| arguments.strip_prefix("Running"))
            .map(str::trim)
            .filter(|detail| !detail.is_empty())
            .map(str::to_owned);
        let arguments = raw_arguments
            .filter(|arguments| *arguments != "Running" && !arguments.starts_with("Running\n"))
            .map(str::to_owned);
        let output = result
            .map(|result| result.display.output.trim())
            .filter(|output| !output.is_empty())
            .map(str::to_owned);
        let status = match result {
            Some(result) if result.is_error => "Failed",
            Some(_) => "Completed",
            None => "Running",
        };
        let status_color = match result {
            Some(result) if result.is_error => theme.danger,
            Some(_) => theme.success,
            None => theme.info,
        };
        let activity = if result.is_none() {
            running_detail
        } else if result.is_some_and(|result| result.is_error) && output.is_none() {
            result.and_then(|result| {
                result.display.progress.last().cloned().or_else(|| {
                    (!result.display.start_message.is_empty())
                        .then(|| result.display.start_message.clone())
                })
            })
        } else {
            None
        };
        let duration_ms = result.map_or(0, |result| result.display.duration_ms);
        let has_details = arguments.is_some() || output.is_some();
        let is_expanded = has_details && self.expanded_tool_cards.contains(&card_id);
        let summary = activity
            .as_deref()
            .or(output.as_deref())
            .or(arguments.as_deref())
            .map(tool_card_summary)
            .filter(|summary| !summary.is_empty());
        let details = is_expanded.then(|| {
            let mut sections = Vec::new();
            if let Some(arguments) = &arguments {
                sections.push(format!(
                    "**Input**\n\n{}",
                    markdown_code_block("json", arguments)
                ));
            }
            if let Some(output) = &output {
                sections.push(format!(
                    "**Output**\n\n{}",
                    markdown_code_block("text", output)
                ));
            }
            sections.join("\n\n")
        });

        v_flex()
            .w_full()
            .min_w(px(0.))
            .overflow_hidden()
            .gap_1()
            .child(
                h_flex()
                    .w_full()
                    .min_w(px(0.))
                    .overflow_hidden()
                    .items_center()
                    .gap_2()
                    .child(
                        h_flex()
                            .flex_shrink_0()
                            .items_center()
                            .gap_2()
                            .child(div().text_xs().font_medium().child(name.to_owned()))
                            .when(status != "Completed", |header| {
                                header.child(div().text_xs().text_color(status_color).child(status))
                            })
                            .when(duration_ms > 0, |header| {
                                header.child(
                                    div()
                                        .text_xs()
                                        .text_color(theme.muted_foreground)
                                        .child(compact_tool_duration(duration_ms)),
                                )
                            }),
                    )
                    .when_some(summary, |header, summary| {
                        header.child(
                            div()
                                .min_w(px(0.))
                                .flex_1()
                                .overflow_hidden()
                                .text_ellipsis()
                                .whitespace_nowrap()
                                .text_xs()
                                .text_color(theme.muted_foreground)
                                .child(summary),
                        )
                    })
                    .child(
                        h_flex()
                            .flex_shrink_0()
                            .items_center()
                            .gap_1()
                            .when(has_details, |actions| {
                                actions.child(
                                    Button::new(("toggle-history-tool", card_id))
                                        .icon(if is_expanded {
                                            IconName::ChevronDown
                                        } else {
                                            IconName::ChevronRight
                                        })
                                        .tooltip(if is_expanded {
                                            "Hide tool details"
                                        } else {
                                            "Show tool details"
                                        })
                                        .ghost()
                                        .compact()
                                        .on_click(cx.listener(move |this, _, _, cx| {
                                            this.toggle_tool_card(card_id, message_index, cx)
                                        })),
                                )
                            })
                            .when_some(arguments.clone(), |actions, arguments| {
                                actions.child(
                                    Button::new(("copy-history-tool-input", card_id))
                                        .icon(IconName::Copy)
                                        .tooltip("Copy tool input")
                                        .ghost()
                                        .compact()
                                        .on_click(move |_, _, cx| {
                                            cx.write_to_clipboard(ClipboardItem::new_string(
                                                arguments.clone(),
                                            ))
                                        }),
                                )
                            })
                            .when_some(output.clone(), |actions, output| {
                                actions.child(
                                    Button::new(("copy-history-tool-output", card_id))
                                        .icon(IconName::Copy)
                                        .tooltip("Copy tool output")
                                        .ghost()
                                        .compact()
                                        .on_click(move |_, _, cx| {
                                            cx.write_to_clipboard(ClipboardItem::new_string(
                                                output.clone(),
                                            ))
                                        }),
                                )
                            }),
                    ),
            )
            .when_some(details, |card, details| {
                card.child(
                    div()
                        .w_full()
                        .min_w(px(0.))
                        .h(px(TOOL_CARD_DETAILS_HEIGHT))
                        .overflow_hidden()
                        .rounded_md()
                        .child(
                            TextView::markdown(
                                ("history-tool-details", card_id),
                                details,
                                window,
                                cx,
                            )
                            .h_full()
                            .selectable(true)
                            .scrollable(true),
                        ),
                )
            })
            .into_any_element()
    }

    fn render_history_content(
        &self,
        message_index: usize,
        message: &ChatMessage,
        theme: &gpui_component::theme::Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Option<AnyElement> {
        if message.history_blocks.is_empty() && message.history_tool_results.is_empty() {
            return None;
        }

        let tool_group_id = (message.render_id << 32) | u32::MAX as u64;
        let tool_group_expanded = self.expanded_tool_cards.contains(&tool_group_id);
        let mut rendered_results = HashSet::new();
        let mut tool_rows = Vec::new();
        let mut tool_count = 0;
        let mut completed_count = 0;
        let mut failed_count = 0;
        let mut duration_ms = 0_i64;

        for (index, block) in message.history_blocks.iter().enumerate() {
            let HistoryBlock::ToolCall(tool) = block else {
                continue;
            };
            let result = message.history_tool_results.iter().find(|result| {
                !tool.tool_call_id.is_empty() && result.tool_call_id == tool.tool_call_id
            });
            tool_count += 1;
            if let Some(result) = result {
                completed_count += 1;
                failed_count += usize::from(result.is_error);
                duration_ms = duration_ms.saturating_add(result.display.duration_ms.max(0));
                rendered_results.insert(result.tool_call_id.clone());
            }
            if tool_group_expanded {
                let card_id = (message.render_id << 32) | index as u64;
                tool_rows.push(self.render_history_tool_card(
                    Some(tool),
                    result,
                    card_id,
                    message_index,
                    theme,
                    window,
                    cx,
                ));
            }
        }

        for (index, result) in message.history_tool_results.iter().enumerate() {
            if rendered_results.contains(&result.tool_call_id) {
                continue;
            }
            tool_count += 1;
            completed_count += 1;
            failed_count += usize::from(result.is_error);
            duration_ms = duration_ms.saturating_add(result.display.duration_ms.max(0));
            if tool_group_expanded {
                let card_id = (message.render_id << 32)
                    | message.history_blocks.len().saturating_add(index) as u64;
                tool_rows.push(self.render_history_tool_card(
                    None,
                    Some(result),
                    card_id,
                    message_index,
                    theme,
                    window,
                    cx,
                ));
            }
        }

        let mut tool_group = (tool_count > 0).then(|| {
            let label = tool_activity_label(tool_count, completed_count, failed_count, duration_ms);
            v_flex()
                .w_full()
                .gap_1()
                .child(
                    Button::new(("toggle-history-tool-group", tool_group_id))
                        .icon(if tool_group_expanded {
                            IconName::ChevronDown
                        } else {
                            IconName::ChevronRight
                        })
                        .label(label)
                        .tooltip(if tool_group_expanded {
                            "Hide tool activity"
                        } else {
                            "Show tool activity"
                        })
                        .ghost()
                        .compact()
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.toggle_tool_card(tool_group_id, message_index, cx)
                        })),
                )
                .when(tool_group_expanded, |group| {
                    group.child(v_flex().w_full().gap_1().pl_2().children(tool_rows))
                })
                .into_any_element()
        });

        let mut content = Vec::new();
        for (index, block) in message.history_blocks.iter().enumerate() {
            let block_id = (message.render_id << 32) | index as u64;
            let element = match block {
                HistoryBlock::Text { text } => {
                    let ordinal = Arc::new(AtomicUsize::new(0));
                    TextView::markdown(("history-text", block_id), text.clone(), window, cx)
                        .selectable(true)
                        .code_block_actions(move |code_block, _, _| {
                            let code = code_block.code().to_string();
                            let number = ordinal.fetch_add(1, Ordering::Relaxed) as u64;
                            let action_id = block_id ^ number;
                            Button::new(("copy-history-code", action_id))
                                .icon(IconName::Copy)
                                .tooltip("Copy")
                                .compact()
                                .ghost()
                                .on_click(move |_, _, cx| {
                                    cx.write_to_clipboard(ClipboardItem::new_string(code.clone()))
                                })
                        })
                        .into_any_element()
                }
                HistoryBlock::Plan { text, complete } => {
                    let status = if *complete { "Complete" } else { "In progress" };
                    v_flex()
                        .gap_2()
                        .p_3()
                        .rounded_lg()
                        .border_1()
                        .border_color(if *complete { theme.success } else { theme.info })
                        .child(
                            h_flex()
                                .items_center()
                                .justify_between()
                                .child(div().text_xs().font_semibold().child("Plan"))
                                .child(
                                    div()
                                        .text_xs()
                                        .text_color(theme.muted_foreground)
                                        .child(status),
                                ),
                        )
                        .child(
                            TextView::markdown(
                                ("history-plan", block_id),
                                text.clone(),
                                window,
                                cx,
                            )
                            .selectable(true),
                        )
                        .into_any_element()
                }
                HistoryBlock::Image(image) => v_flex()
                    .gap_1()
                    .p_2()
                    .rounded_lg()
                    .border_1()
                    .border_color(theme.border)
                    .child(
                        img(Arc::clone(&image.preview))
                            .max_w(px(420.))
                            .max_h(px(320.))
                            .rounded_md(),
                    )
                    .child(
                        div()
                            .text_xs()
                            .text_color(theme.muted_foreground)
                            .child(format!(
                                "{} · {} KiB",
                                image.mime_type,
                                image.data.len().div_ceil(1024)
                            )),
                    )
                    .into_any_element(),
                HistoryBlock::ToolCall(_) => {
                    if let Some(group) = tool_group.take() {
                        content.push(group);
                    }
                    continue;
                }
            };
            content.push(element);
        }

        if let Some(group) = tool_group {
            content.push(group);
        }

        Some(
            v_flex()
                .w_full()
                .gap_1()
                .children(content)
                .into_any_element(),
        )
    }

    fn render_message(
        &self,
        index: usize,
        message: &ChatMessage,
        theme: &gpui_component::theme::Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let render_id = message.render_id;
        let has_history =
            !message.history_blocks.is_empty() || !message.history_tool_results.is_empty();
        match message.role {
            ChatRole::User => {
                let copy_text = chat_message_copy_text(message);
                let history = self.render_history_content(index, message, theme, window, cx);
                h_flex()
                    .w_full()
                    .justify_end()
                    .child(
                        v_flex()
                            .max_w(px(620.))
                            .gap_1()
                            .px_4()
                            .py_2()
                            .rounded_xl()
                            .bg(theme.secondary)
                            .text_color(theme.foreground)
                            .text_sm()
                            .when(!copy_text.is_empty(), |bubble| {
                                let copy_text = copy_text.clone();
                                bubble.child(
                                    h_flex().justify_end().child(
                                        Button::new(("copy-user-message", render_id))
                                            .icon(IconName::Copy)
                                            .tooltip("Copy")
                                            .compact()
                                            .ghost()
                                            .on_click(move |_, _, cx| {
                                                cx.write_to_clipboard(ClipboardItem::new_string(
                                                    copy_text.clone(),
                                                ))
                                            }),
                                    ),
                                )
                            })
                            .when(!has_history && !message.text.is_empty(), |bubble| {
                                bubble.child(
                                    TextView::html(
                                        ("user-text", render_id),
                                        plain_text_html(&message.text),
                                        window,
                                        cx,
                                    )
                                    .selectable(true),
                                )
                            })
                            .when_some(history, |bubble, content| bubble.child(content)),
                    )
                    .into_any_element()
            }
            ChatRole::Assistant => {
                let history = self.render_history_content(index, message, theme, window, cx);
                let copy_text = (!message.streaming && !has_history)
                    .then(|| chat_message_copy_text(message))
                    .unwrap_or_default();
                let show_message_copy = !copy_text.is_empty();
                let code_block_ordinal = Arc::new(AtomicUsize::new(0));
                v_flex()
                    .w_full()
                    .min_w(px(0.))
                    .gap_2()
                    .when(message.streaming || show_message_copy, |column| {
                        column.child(
                            h_flex()
                                .w_full()
                                .items_center()
                                .justify_between()
                                .when(message.streaming, |row| {
                                    row.child(
                                        div()
                                            .text_xs()
                                            .text_color(theme.info)
                                            .child("Snow is working…"),
                                    )
                                })
                                .when(show_message_copy, |row| {
                                    let copy_text = copy_text.clone();
                                    row.child(
                                        Button::new(("copy-assistant-message", render_id))
                                            .icon(IconName::Copy)
                                            .tooltip("Copy")
                                            .compact()
                                            .ghost()
                                            .on_click(move |_, _, cx| {
                                                cx.write_to_clipboard(ClipboardItem::new_string(
                                                    copy_text.clone(),
                                                ))
                                            }),
                                    )
                                }),
                        )
                    })
                    .when(!has_history, |column| {
                        column.child(
                            TextView::markdown(
                                ("assistant-markdown", render_id),
                                message.presentation_text.clone(),
                                window,
                                cx,
                            )
                            .selectable(true)
                            .code_block_actions(
                                move |code_block, _, _| {
                                    let code = code_block.code().to_string();
                                    let ordinal =
                                        code_block_ordinal.fetch_add(1, Ordering::Relaxed);
                                    let action_id = (render_id << 32) | ordinal as u64;
                                    Button::new(("copy-code", action_id))
                                        .icon(IconName::Copy)
                                        .tooltip("Copy")
                                        .compact()
                                        .ghost()
                                        .on_click(move |_, _, cx| {
                                            cx.write_to_clipboard(ClipboardItem::new_string(
                                                code.clone(),
                                            ))
                                        })
                                },
                            ),
                        )
                    })
                    .when_some(history, |column, content| column.child(content))
                    .into_any_element()
            }
            ChatRole::System => {
                let copy_text = chat_message_copy_text(message);
                let history = self.render_history_content(index, message, theme, window, cx);
                h_flex()
                    .w_full()
                    .justify_center()
                    .child(
                        v_flex()
                            .w_full()
                            .max_w(px(620.))
                            .gap_2()
                            .when(!has_history && !message.text.is_empty(), |column| {
                                let copy_text = copy_text.clone();
                                column.child(
                                    h_flex()
                                        .justify_between()
                                        .px_3()
                                        .py_1()
                                        .rounded_lg()
                                        .bg(theme.secondary)
                                        .text_xs()
                                        .text_color(theme.muted_foreground)
                                        .child(
                                            div().min_w(px(0.)).flex_1().child(
                                                TextView::html(
                                                    ("system-text", render_id),
                                                    plain_text_html(&message.text),
                                                    window,
                                                    cx,
                                                )
                                                .selectable(true),
                                            ),
                                        )
                                        .when(!copy_text.is_empty(), |row| {
                                            row.child(
                                                Button::new(("copy-system-message", render_id))
                                                    .icon(IconName::Copy)
                                                    .tooltip("Copy")
                                                    .compact()
                                                    .ghost()
                                                    .on_click(move |_, _, cx| {
                                                        cx.write_to_clipboard(
                                                            ClipboardItem::new_string(
                                                                copy_text.clone(),
                                                            ),
                                                        )
                                                    }),
                                            )
                                        }),
                                )
                            })
                            .when(has_history && !copy_text.is_empty(), |column| {
                                let copy_text = copy_text.clone();
                                column.child(
                                    h_flex().justify_end().child(
                                        Button::new(("copy-system-message", render_id))
                                            .icon(IconName::Copy)
                                            .tooltip("Copy")
                                            .compact()
                                            .ghost()
                                            .on_click(move |_, _, cx| {
                                                cx.write_to_clipboard(ClipboardItem::new_string(
                                                    copy_text.clone(),
                                                ))
                                            }),
                                    ),
                                )
                            })
                            .when_some(history, |column, content| column.child(content)),
                    )
                    .into_any_element()
            }
        }
    }

    fn render_interaction_card(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let theme = cx.theme();
        match self.state.active_interaction.as_ref()? {
            ActiveInteraction::Permission(interaction) => {
                let pending = interaction.pending.is_some();
                let args = bounded_display(
                    &serde_json::to_string_pretty(&interaction.request.args)
                        .unwrap_or_else(|_| "<unavailable>".into()),
                    4_000,
                );
                let decision_button =
                    |id: &'static str,
                     label: &'static str,
                     decision: PermissionDecision,
                     danger: bool| {
                        Button::new(id)
                            .label(label)
                            .when(danger, |button| button.danger())
                            .when(!danger, |button| button.ghost())
                            .disabled(pending)
                            .on_click(cx.listener(move |this, _, _, cx| {
                                this.resolve_permission(decision, cx)
                            }))
                    };
                Some(
                    v_flex()
                        .mx_auto()
                        .mb_3()
                        .w_full()
                        .max_w(px(CONVERSATION_WIDTH))
                        .gap_3()
                        .p_4()
                        .rounded_xl()
                        .border_1()
                        .border_color(theme.warning)
                        .bg(theme.background)
                        .child(
                            h_flex()
                                .justify_between()
                                .child(div().font_semibold().child("Trusted permission required"))
                                .child(div().text_xs().text_color(theme.warning).child(
                                    if pending {
                                        "Waiting for Snow…"
                                    } else {
                                        "Review before allowing"
                                    },
                                )),
                        )
                        .child(
                            h_flex()
                                .gap_2()
                                .when_some(interaction.request.agent.as_ref(), |row, agent| {
                                    row.child(
                                        div()
                                            .px_2()
                                            .rounded_full()
                                            .bg(theme.secondary)
                                            .text_xs()
                                            .child(format!(
                                                "{}{}",
                                                agent.path,
                                                if agent.role.is_empty() {
                                                    String::new()
                                                } else {
                                                    format!(" · {}", agent.role)
                                                }
                                            )),
                                    )
                                })
                                .child(
                                    div()
                                        .text_sm()
                                        .font_medium()
                                        .child(bounded_display(&interaction.request.tool, 256)),
                                )
                                .child(
                                    div()
                                        .px_2()
                                        .rounded_full()
                                        .bg(theme.secondary)
                                        .text_xs()
                                        .child(bounded_display(&interaction.request.risk, 64)),
                                ),
                        )
                        .when(!interaction.request.reason.is_empty(), |card| {
                            card.child(
                                div()
                                    .text_sm()
                                    .text_color(theme.muted_foreground)
                                    .child(bounded_display(&interaction.request.reason, 1_000)),
                            )
                        })
                        .when(!interaction.request.paths.is_empty(), |card| {
                            card.child(div().text_xs().text_color(theme.muted_foreground).child(
                                format!("Paths: {}", bounded_paths(&interaction.request.paths)),
                            ))
                        })
                        .child(
                            div()
                                .max_h(px(120.))
                                .overflow_y_scrollbar()
                                .p_2()
                                .rounded_lg()
                                .bg(theme.secondary)
                                .text_xs()
                                .font_family("monospace")
                                .child(args),
                        )
                        .child(
                            h_flex()
                                .flex_wrap()
                                .gap_2()
                                .child(decision_button(
                                    "permission-once",
                                    "Allow once",
                                    PermissionDecision::Allow,
                                    false,
                                ))
                                .child(decision_button(
                                    "permission-session",
                                    "Allow for session",
                                    PermissionDecision::AllowSession,
                                    false,
                                ))
                                .child(decision_button(
                                    "permission-always",
                                    "Always allow",
                                    PermissionDecision::AllowAlways,
                                    false,
                                ))
                                .child(decision_button(
                                    "permission-deny",
                                    "Deny",
                                    PermissionDecision::Deny,
                                    true,
                                )),
                        )
                        .into_any_element(),
                )
            }
            ActiveInteraction::UserInput(interaction) => {
                let question = interaction.question();
                let draft = interaction.draft();
                let pending = interaction.pending.is_some();
                let question_index = interaction.question_index;
                let question_count = interaction.request.questions.len();
                let last = question_index + 1 == question_count;
                Some(
                    v_flex()
                        .mx_auto()
                        .mb_3()
                        .w_full()
                        .max_w(px(CONVERSATION_WIDTH))
                        .gap_3()
                        .p_4()
                        .rounded_xl()
                        .border_1()
                        .border_color(theme.info)
                        .bg(theme.background)
                        .child(
                            h_flex()
                                .justify_between()
                                .child(
                                    div()
                                        .font_semibold()
                                        .child(bounded_display(&question.header, 80)),
                                )
                                .child(div().text_xs().text_color(theme.muted_foreground).child(
                                    if pending {
                                        "Waiting for Snow…".into()
                                    } else {
                                        format!(
                                            "Question {} of {question_count}",
                                            question_index + 1
                                        )
                                    },
                                )),
                        )
                        .when_some(interaction.request.agent.as_ref(), |card, agent| {
                            card.child(div().text_xs().text_color(theme.muted_foreground).child(
                                format!(
                                    "Requested by {}{}",
                                    agent.path,
                                    if agent.role.is_empty() {
                                        String::new()
                                    } else {
                                        format!(" · {}", agent.role)
                                    }
                                ),
                            ))
                        })
                        .child(
                            div()
                                .text_sm()
                                .child(bounded_display(&question.question, 1_000)),
                        )
                        .children(question.options.iter().enumerate().map(|(index, option)| {
                            let label = option.label.clone();
                            let selected = !draft.use_other
                                && draft.selected.as_deref() == Some(option.label.as_str());
                            v_flex()
                                .w_full()
                                .gap_1()
                                .child(
                                    Button::new((
                                        "user-input-option",
                                        question_index * 1024 + index,
                                    ))
                                    .w_full()
                                    .label(if selected {
                                        format!("✓ {}", bounded_display(&option.label, 100))
                                    } else {
                                        bounded_display(&option.label, 100)
                                    })
                                    .when(selected, |button| button.primary())
                                    .when(!selected, |button| button.ghost())
                                    .disabled(pending)
                                    .on_click(cx.listener(
                                        move |this, _, _, cx| {
                                            this.select_user_input_option(&label, cx)
                                        },
                                    )),
                                )
                                .when(!option.description.is_empty(), |option_row| {
                                    option_row.child(
                                        div()
                                            .px_3()
                                            .text_xs()
                                            .text_color(theme.muted_foreground)
                                            .child(bounded_display(&option.description, 300)),
                                    )
                                })
                        }))
                        .when(!question.options.is_empty(), |card| {
                            card.child(
                                Button::new(("user-input-other", question_index))
                                    .w_full()
                                    .label(if draft.use_other {
                                        "✓ Other"
                                    } else {
                                        "Other"
                                    })
                                    .when(draft.use_other, |button| button.primary())
                                    .when(!draft.use_other, |button| button.ghost())
                                    .disabled(pending)
                                    .on_click(cx.listener(|this, _, window, cx| {
                                        this.select_user_input_other(window, cx)
                                    })),
                            )
                        })
                        .when(draft.use_other, |card| {
                            card.child(Input::new(&self.interaction_input).disabled(pending))
                        })
                        .when_some(interaction.validation_error.as_ref(), |card, error| {
                            card.child(
                                div()
                                    .text_xs()
                                    .text_color(theme.danger)
                                    .child(error.clone()),
                            )
                        })
                        .child(
                            h_flex()
                                .justify_between()
                                .child(
                                    Button::new(("user-input-decline", question_index))
                                        .label("Decline")
                                        .danger()
                                        .disabled(pending)
                                        .on_click(cx.listener(|this, _, window, cx| {
                                            this.decline_user_input(window, cx)
                                        })),
                                )
                                .child(
                                    h_flex()
                                        .gap_2()
                                        .when(question_index > 0, |buttons| {
                                            buttons.child(
                                                Button::new(("user-input-prev", question_index))
                                                    .label("Previous")
                                                    .ghost()
                                                    .disabled(pending)
                                                    .on_click(cx.listener(
                                                        |this, _, window, cx| {
                                                            this.move_user_input_question(
                                                                -1, window, cx,
                                                            )
                                                        },
                                                    )),
                                            )
                                        })
                                        .child(if last {
                                            Button::new(("user-input-submit", question_index))
                                                .label("Submit")
                                                .primary()
                                                .disabled(pending)
                                                .on_click(cx.listener(|this, _, window, cx| {
                                                    this.submit_user_input(window, cx)
                                                }))
                                        } else {
                                            Button::new(("user-input-next", question_index))
                                                .label("Next")
                                                .primary()
                                                .disabled(pending)
                                                .on_click(cx.listener(|this, _, window, cx| {
                                                    this.move_user_input_question(1, window, cx)
                                                }))
                                        }),
                                ),
                        )
                        .into_any_element(),
                )
            }
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

    fn render_provider_picker(&self, cx: &mut Context<Self>) -> AnyElement {
        let theme = cx.theme();
        let can_switch = self.state.can_switch_provider();
        let catalog = self.provider_catalog();
        let results = search_provider_catalog(&catalog, &self.composer_picker.search.query);
        let empty_message = provider_picker_empty_message(
            catalog.len(),
            results.len(),
            &self.composer_picker.search.query,
        );

        v_flex()
            .key_context("PickerSearch DesktopPicker")
            .w(px(420.))
            .max_h(px(PICKER_MAX_HEIGHT))
            .p_2()
            .gap_1()
            .rounded_xl()
            .border_1()
            .border_color(theme.border)
            .bg(theme.background)
            .child(Input::new(&self.picker_search_input))
            .child(
                v_flex()
                    .min_h(px(0.))
                    .overflow_y_scrollbar()
                    .children(results.into_iter().enumerate().filter_map(
                        |(result_index, index)| {
                            let item = catalog.get(index)?.clone();
                            let provider = item.id.clone();
                            let selected = item.active;
                            let highlighted =
                                result_index == self.composer_picker.search.highlighted;
                            let retries_failure = selected
                                && matches!(self.state.connection, ConnectionState::Failed(_));
                            let authentication_attention = item.status.authentication_attention();
                            Some(
                                v_flex()
                                    .w_full()
                                    .gap_1()
                                    .px_2()
                                    .py_1()
                                    .rounded_lg()
                                    .when(selected, |row| row.bg(theme.secondary))
                                    .child(
                                        Button::new(domain_element_id("provider-picker", &item.id))
                                            .w_full()
                                            .label(if highlighted {
                                                format!("› {}", item.label)
                                            } else {
                                                item.label
                                            })
                                            .ghost()
                                            .disabled((selected && !retries_failure) || !can_switch)
                                            .on_click(cx.listener(move |this, _, window, cx| {
                                                this.select_provider(&provider, window, cx);
                                            })),
                                    )
                                    .when_some(authentication_attention, |row, status| {
                                        row.child(
                                            div()
                                                .px_2()
                                                .pb_1()
                                                .text_xs()
                                                .text_color(theme.warning)
                                                .child(status),
                                        )
                                    }),
                            )
                        },
                    ))
                    .when_some(empty_message, |list, message| {
                        list.child(
                            div()
                                .px_3()
                                .py_3()
                                .text_xs()
                                .text_color(theme.muted_foreground)
                                .child(message),
                        )
                    }),
            )
            .into_any_element()
    }

    fn render_model_picker(&self, cx: &mut Context<Self>) -> AnyElement {
        if !is_user_visible_provider(&self.provider) {
            return div().into_any_element();
        }
        let theme = cx.theme().clone();
        let can_switch = self.state.can_switch_model();
        let results = search_models(&self.state.models, &self.composer_picker.search.query);
        let manual = manual_model_id(&self.state.models, &self.composer_picker.search.query);
        let result_count = results.len();
        let manual_highlighted = manual_model_row_highlighted(
            result_count,
            manual.is_some(),
            self.composer_picker.search.highlighted,
        );

        v_flex()
            .key_context("PickerSearch DesktopPicker")
            .w(px(360.))
            .max_h(px(PICKER_MAX_HEIGHT))
            .p_2()
            .gap_1()
            .rounded_xl()
            .border_1()
            .border_color(theme.border)
            .bg(theme.background)
            .child(Input::new(&self.picker_search_input))
            .child(
                v_flex()
                    .min_h(px(0.))
                    .overflow_y_scrollbar()
                    .children(
                        results
                            .into_iter()
                            .enumerate()
                            .map(|(result_index, index)| {
                                let model = &self.state.models[index];
                                let model_id = model.id.clone();
                                let selected = model.id == self.state.current_model;
                                let highlighted =
                                    result_index == self.composer_picker.search.highlighted;
                                let presentation = model_presentation(model);
                                let label = presentation.label.clone();
                                v_flex()
                                    .w_full()
                                    .gap_1()
                                    .p_1()
                                    .rounded_lg()
                                    .when(selected, |row| row.bg(theme.secondary))
                                    .child(if selected {
                                        h_flex()
                                            .w_full()
                                            .justify_between()
                                            .px_2()
                                            .py_1()
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
                                        Button::new(domain_element_id("model-picker", &model.id))
                                            .w_full()
                                            .label(if highlighted {
                                                format!("› {label}")
                                            } else {
                                                label
                                            })
                                            .ghost()
                                            .disabled(!can_switch)
                                            .on_click(cx.listener(move |this, _, window, cx| {
                                                this.select_model(&model_id, window, cx);
                                            }))
                                            .into_any_element()
                                    })
                                    .into_any_element()
                            }),
                    )
                    .when_some(manual, |models, manual| {
                        let selected = manual == self.state.current_model;
                        models.child(
                            Button::new("manual-model")
                                .w_full()
                                .selected(manual_highlighted)
                                .when(manual_highlighted, |row| row.bg(theme.secondary))
                                .label(if selected {
                                    format!(
                                        "{}Manual: {manual} · Selected",
                                        if manual_highlighted { "› " } else { "" }
                                    )
                                } else {
                                    format!(
                                        "{}Use manual model ID: {manual}",
                                        if manual_highlighted { "› " } else { "" }
                                    )
                                })
                                .ghost()
                                .disabled(!can_switch || selected)
                                .on_click(cx.listener(move |this, _, window, cx| {
                                    this.select_model(&manual, window, cx)
                                })),
                        )
                    })
                    .when(
                        result_count == 0 && self.composer_picker.search.query.trim().is_empty(),
                        |models| {
                            models.child(
                                div()
                                    .px_3()
                                    .py_2()
                                    .text_xs()
                                    .text_color(theme.muted_foreground)
                                    .child(
                                        "No discovered models. Type a model ID to use it manually.",
                                    ),
                            )
                        },
                    ),
            )
            .into_any_element()
    }

    fn render_thinking_picker(&self, cx: &mut Context<Self>) -> AnyElement {
        let theme = cx.theme();
        let can_switch = self.state.can_switch_thinking();

        v_flex()
            .key_context("DesktopPicker")
            .w(px(210.))
            .max_h(px(PICKER_MAX_HEIGHT))
            .overflow_y_scrollbar()
            .p_1()
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
                        let highlighted = index == self.composer_picker.search.highlighted;
                        Button::new(("thinking-picker", index))
                            .w_full()
                            .h(px(38.))
                            .min_w(px(0.))
                            .px_3()
                            .justify_start()
                            .selected(selected || highlighted)
                            .when(selected || highlighted, |row| row.bg(theme.secondary))
                            .ghost()
                            .disabled(!can_switch)
                            .child(
                                h_flex()
                                    .w_full()
                                    .min_w(px(0.))
                                    .justify_between()
                                    .child(
                                        div().text_sm().font_medium().child(thinking_label(level)),
                                    )
                                    .when(selected, |row| {
                                        row.child(
                                            div()
                                                .text_sm()
                                                .text_color(theme.muted_foreground)
                                                .child("✓"),
                                        )
                                    }),
                            )
                            .on_click(cx.listener(move |this, _, window, cx| {
                                this.select_thinking(&target, window, cx);
                            }))
                    }),
            )
            .into_any_element()
    }

    fn render_permission_picker(&self, cx: &mut Context<Self>) -> AnyElement {
        let theme = cx.theme();
        let can_switch = self.state.active_interaction.is_none();

        v_flex()
            .key_context("DesktopPicker")
            .w(px(190.))
            .p_2()
            .gap_1()
            .rounded_xl()
            .border_1()
            .border_color(theme.border)
            .bg(theme.background)
            .children(
                PERMISSION_MODES
                    .into_iter()
                    .enumerate()
                    .map(|(index, mode)| {
                        let selected = mode == self.state.permission_mode;
                        let highlighted = index == self.composer_picker.search.highlighted;
                        let label = match mode {
                            "ask" => "Ask",
                            "allow" => "Allow",
                            "deny" => "Deny",
                            _ => mode,
                        };
                        Button::new(("permission-picker", index))
                            .w_full()
                            .selected(selected || highlighted)
                            .when(selected || highlighted, |row| row.bg(theme.secondary))
                            .label(if selected {
                                format!("{label}  ✓")
                            } else {
                                label.to_owned()
                            })
                            .ghost()
                            .disabled(!can_switch)
                            .on_click(cx.listener(move |this, _, window, cx| {
                                this.select_permission_mode(mode, window, cx);
                            }))
                    }),
            )
            .into_any_element()
    }

    fn render_command_suggestions(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        if !self.slash_selection.visible || self.slash_selection.matches.is_empty() {
            return None;
        }
        let theme = cx.theme();
        Some(
            v_flex()
                .w_full()
                .max_h(px(PICKER_MAX_HEIGHT))
                .overflow_hidden()
                .rounded_lg()
                .border_1()
                .border_color(theme.border)
                .bg(theme.popover)
                .p_2()
                .gap_1()
                .children(
                    self.slash_selection
                        .matches
                        .iter()
                        .take(MAX_COMPOSER_COMPLETIONS)
                        .enumerate()
                        .map(|(index, command)| {
                            let selected_command = command.name.clone();
                            let selected = index == self.slash_selection.selected;
                            Button::new(("command", index))
                                .w_full()
                                .ghost()
                                .selected(selected)
                                .when(selected, |row| row.bg(theme.secondary))
                                .child(
                                    h_flex()
                                        .w_full()
                                        .min_w(px(0.))
                                        .gap_3()
                                        .child(
                                            div()
                                                .w(px(128.))
                                                .flex_shrink_0()
                                                .text_sm()
                                                .font_medium()
                                                .text_color(theme.foreground)
                                                .child(command.name.clone()),
                                        )
                                        .child(
                                            div()
                                                .min_w(px(0.))
                                                .flex_1()
                                                .overflow_hidden()
                                                .text_ellipsis()
                                                .text_sm()
                                                .text_color(theme.muted_foreground)
                                                .child(command.description.clone()),
                                        ),
                                )
                                .on_click(cx.listener(move |this, _, window, cx| {
                                    this.select_command_completion(&selected_command, window, cx)
                                }))
                        }),
                )
                .into_any_element(),
        )
    }

    fn render_mention_suggestions(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let token_start = self.mention_token_start?;
        if self.mention_matches.is_empty() {
            return None;
        }
        let theme = cx.theme();
        Some(
            v_flex()
                .w_full()
                .max_h(px(PICKER_MAX_HEIGHT))
                .overflow_hidden()
                .rounded_lg()
                .border_1()
                .border_color(theme.border)
                .bg(theme.popover)
                .p_2()
                .children(
                    self.mention_matches
                        .iter()
                        .take(MAX_COMPOSER_COMPLETIONS)
                        .enumerate()
                        .map(|(index, path)| {
                            let selected_path = path.clone();
                            let marker = if index == self.mention_selected {
                                "› "
                            } else {
                                "  "
                            };
                            Button::new(("mention", index))
                                .label(format!("{marker}@{path}"))
                                .ghost()
                                .on_click(cx.listener(move |this, _, window, cx| {
                                    this.select_mention_completion(
                                        &selected_path,
                                        token_start,
                                        window,
                                        cx,
                                    )
                                }))
                        }),
                )
                .into_any_element(),
        )
    }

    fn render_skill_suggestions(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let value = self.input.read(cx).value().to_string();
        let completion = complete_skills(&value, &self.skill_catalog, CompletionLimits::default())?;
        if completion.matches.is_empty() {
            return None;
        }
        let token_start = completion.token_start;
        let theme = cx.theme();
        Some(
            v_flex()
                .w_full()
                .max_h(px(PICKER_MAX_HEIGHT))
                .overflow_hidden()
                .rounded_lg()
                .border_1()
                .border_color(theme.border)
                .bg(theme.popover)
                .p_2()
                .children(
                    completion
                        .matches
                        .into_iter()
                        .take(MAX_COMPOSER_COMPLETIONS)
                        .enumerate()
                        .map(|(index, skill)| {
                            let name = skill.name.clone();
                            let selected = index == self.skill_selection.selected;
                            Button::new(("skill", index))
                                .selected(selected)
                                .when(selected, |row| row.bg(theme.secondary))
                                .label(format!(
                                    "{}${:<18}  {}",
                                    if selected { "› " } else { "" },
                                    skill.name,
                                    skill.description
                                ))
                                .ghost()
                                .on_click(cx.listener(move |this, _, window, cx| {
                                    this.select_skill_completion(&name, token_start, window, cx)
                                }))
                        }),
                )
                .into_any_element(),
        )
    }

    fn render_composer(
        &self,
        footer_layout: ComposerFooterLayout,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let suggestion = self.active_composer_suggestion(cx);
        let theme = cx.theme();
        let can_edit_composer = self.state.can_edit_composer();
        let can_send = self.state.can_send();
        let can_abort = self.state.can_abort();
        let can_switch_provider = self.state.can_switch_provider();
        let can_open_model_picker = can_open_model_picker(&self.provider, self.state.can_send());
        let can_open_thinking_picker = self.state.can_switch_thinking();
        let wrapped_footer = footer_layout == ComposerFooterLayout::Wrapped;

        v_flex()
            .key_context(if self.state.active_prompt.is_some() {
                "DesktopComposer DesktopComposerBusy"
            } else {
                "DesktopComposer DesktopComposerIdle"
            })
            .w_full()
            .flex_shrink_0()
            .items_center()
            .px_6()
            .pb_6()
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
                    .child(
                        v_flex()
                            .w_full()
                            .gap_3()
                            .p_4()
                            .rounded_xl()
                            .border_1()
                            .border_color(theme.border)
                            .bg(theme.secondary)
                            .when(!self.paste_store.attachments().is_empty(), |composer| {
                                composer.child(h_flex().w_full().flex_wrap().gap_1().children(
                                    self.paste_store.attachments().iter().map(|paste| {
                                        let id = paste.id;
                                        let line_count = paste.text().lines().count().max(1);
                                        Button::new(("collapsed-paste", id as usize))
                                            .label(format!(
                                                "Paste #{} · {} lines · {} KiB  ×",
                                                id + 1,
                                                line_count,
                                                paste.text().len().div_ceil(1024)
                                            ))
                                            .ghost()
                                            .on_click(cx.listener(move |this, _, window, cx| {
                                                this.remove_collapsed_paste(id, window, cx)
                                            }))
                                    }),
                                ))
                            })
                            .when(!self.attachments.is_empty(), |composer| {
                                composer.child(h_flex().w_full().flex_wrap().gap_1().children(
                                    self.attachments.images().iter().enumerate().map(
                                        |(index, image)| {
                                            let format = image_format_for_mime(image.mime_type())
                                                .expect("attachment MIME was validated");
                                            h_flex()
                                                .items_center()
                                                .gap_1()
                                                .p_1()
                                                .rounded_lg()
                                                .border_1()
                                                .border_color(theme.border)
                                                .child(
                                                    img(Arc::new(Image::from_bytes(
                                                        format,
                                                        image.data().to_vec(),
                                                    )))
                                                    .size(px(36.))
                                                    .rounded_md(),
                                                )
                                                .child(
                                                    Button::new(("attachment", index))
                                                        .label(format!(
                                                            "Image #{} · {} · {} KiB  ×",
                                                            index + 1,
                                                            image.label(),
                                                            image.len().div_ceil(1024)
                                                        ))
                                                        .ghost()
                                                        .on_click(cx.listener(
                                                            move |this, _, _, cx| {
                                                                this.remove_attachment(index, cx)
                                                            },
                                                        )),
                                                )
                                        },
                                    ),
                                ))
                            })
                            .child({
                                let workspace = cx.entity().downgrade();
                                let suggestion_content = suggestion
                                    .and_then(|suggestion| match suggestion {
                                        ComposerSuggestion::Slash => {
                                            self.render_command_suggestions(cx)
                                        }
                                        ComposerSuggestion::Mention => {
                                            self.render_mention_suggestions(cx)
                                        }
                                        ComposerSuggestion::Skill => {
                                            self.render_skill_suggestions(cx)
                                        }
                                    })
                                    .unwrap_or_else(|| div().into_any_element());
                                Popover::new("composer-suggestion-popover")
                                    .anchor(Corner::BottomLeft)
                                    .track_focus(&self.input.read(cx).focus_handle(cx))
                                    .open(suggestion.is_some())
                                    .on_open_change(move |open, window, cx| {
                                        let _ = workspace.update(cx, |this, cx| {
                                            this.set_input_suggestion_popover_open(
                                                *open, window, cx,
                                            )
                                        });
                                    })
                                    .trigger(
                                        Input::new(&self.input)
                                            .w_full()
                                            .min_h(px(72.))
                                            .appearance(false)
                                            .disabled(!can_edit_composer),
                                    )
                                    .child(suggestion_content)
                            })
                            .child(
                                h_flex()
                                    .w_full()
                                    .items_center()
                                    .justify_between()
                                    .gap_3()
                                    .flex_wrap()
                                    .child(
                                        h_flex()
                                            .min_w(px(0.))
                                            .items_center()
                                            .gap_1()
                                            .flex_wrap()
                                            .when(wrapped_footer, |controls| controls.w_full())
                                            .when(!wrapped_footer, |controls| controls.flex_1())
                                            .child({
                                                let workspace = cx.entity().downgrade();
                                                Popover::new("provider-menu-popover")
                                                    .anchor(Corner::BottomLeft)
                                                    .open(
                                                        self.composer_picker.active
                                                            == Some(ComposerPicker::Provider),
                                                    )
                                                    .on_open_change(move |open, window, cx| {
                                                        let _ = workspace.update(cx, |this, cx| {
                                                            this.set_composer_picker_open(
                                                                ComposerPicker::Provider,
                                                                *open,
                                                                window,
                                                                cx,
                                                            )
                                                        });
                                                    })
                                                    .trigger(
                                                        Button::new("provider-menu")
                                                            .label(format!(
                                                                "{}  ▾",
                                                                bounded_display(
                                                                    &self.provider_label(),
                                                                    24,
                                                                )
                                                            ))
                                                            .min_w(px(150.))
                                                            .max_w(px(200.))
                                                            .flex_none()
                                                            .ghost()
                                                            .disabled(!can_switch_provider),
                                                    )
                                                    .child(self.render_provider_picker(cx))
                                            })
                                            .child({
                                                let workspace = cx.entity().downgrade();
                                                Popover::new("model-menu-popover")
                                                    .anchor(Corner::BottomLeft)
                                                    .open(
                                                        self.composer_picker.active
                                                            == Some(ComposerPicker::Model),
                                                    )
                                                    .on_open_change(move |open, window, cx| {
                                                        let _ = workspace.update(cx, |this, cx| {
                                                            this.set_composer_picker_open(
                                                                ComposerPicker::Model,
                                                                *open,
                                                                window,
                                                                cx,
                                                            )
                                                        });
                                                    })
                                                    .trigger(
                                                        Button::new("model-menu")
                                                            .label(format!(
                                                                "{}  ▾",
                                                                bounded_display(
                                                                    &self.model_label(),
                                                                    28,
                                                                )
                                                            ))
                                                            .max_w(px(230.))
                                                            .ghost()
                                                            .disabled(!can_open_model_picker),
                                                    )
                                                    .child(self.render_model_picker(cx))
                                            })
                                            .child({
                                                let workspace = cx.entity().downgrade();
                                                Popover::new("thinking-menu-popover")
                                                    .appearance(false)
                                                    .anchor(Corner::BottomLeft)
                                                    .open(
                                                        self.composer_picker.active
                                                            == Some(ComposerPicker::Thinking),
                                                    )
                                                    .on_open_change(move |open, window, cx| {
                                                        let _ = workspace.update(cx, |this, cx| {
                                                            this.set_composer_picker_open(
                                                                ComposerPicker::Thinking,
                                                                *open,
                                                                window,
                                                                cx,
                                                            )
                                                        });
                                                    })
                                                    .trigger(
                                                        Button::new("thinking-menu")
                                                            .label(format!(
                                                                "Thinking: {}  ▾",
                                                                thinking_label(
                                                                    &self.state.current_thinking
                                                                )
                                                            ))
                                                            .ghost()
                                                            .disabled(!can_open_thinking_picker),
                                                    )
                                                    .child(self.render_thinking_picker(cx))
                                            })
                                            .child({
                                                let workspace = cx.entity().downgrade();
                                                Popover::new("permission-menu-popover")
                                                    .appearance(false)
                                                    .anchor(Corner::BottomLeft)
                                                    .open(
                                                        self.composer_picker.active
                                                            == Some(ComposerPicker::Permission),
                                                    )
                                                    .on_open_change(move |open, window, cx| {
                                                        let _ = workspace.update(cx, |this, cx| {
                                                            this.set_composer_picker_open(
                                                                ComposerPicker::Permission,
                                                                *open,
                                                                window,
                                                                cx,
                                                            )
                                                        });
                                                    })
                                                    .trigger(
                                                        Button::new("permission-menu")
                                                            .label(format!(
                                                                "Permission: {}  ▾",
                                                                bounded_display(
                                                                    display_value(
                                                                        &self.state.permission_mode
                                                                    ),
                                                                    18,
                                                                )
                                                            ))
                                                            .max_w(px(190.))
                                                            .ghost()
                                                            .disabled(
                                                                self.state
                                                                    .active_interaction
                                                                    .is_some(),
                                                            ),
                                                    )
                                                    .child(self.render_permission_picker(cx))
                                            }),
                                    )
                                    .child(
                                        h_flex()
                                            .items_center()
                                            .gap_1()
                                            .when(wrapped_footer, |actions| {
                                                actions.w_full().justify_end()
                                            })
                                            .child(
                                                Button::new("paste-image")
                                                    .label("Attach image")
                                                    .ghost()
                                                    .disabled(!can_send)
                                                    .on_click(cx.listener(
                                                        |this, _, window, cx| {
                                                            this.load_attachment(
                                                                AttachmentSource::Clipboard,
                                                                window,
                                                                cx,
                                                            )
                                                        },
                                                    )),
                                            )
                                            .child(match self.state.composer_action() {
                                                ComposerAction::Stop => {
                                                    Button::new("stop")
                                                        .danger()
                                                        .label("■")
                                                        .size(px(36.))
                                                        .rounded_full()
                                                        .disabled(!can_abort)
                                                        .on_click(cx.listener(|this, _, _, cx| {
                                                            this.abort(cx)
                                                        }))
                                                        .into_any_element()
                                                }
                                                ComposerAction::Send => Button::new("send")
                                                    .primary()
                                                    .label("↑")
                                                    .size(px(36.))
                                                    .rounded_full()
                                                    .disabled(!can_send)
                                                    .on_click(cx.listener(|this, _, window, cx| {
                                                        this.submit(window, cx)
                                                    }))
                                                    .into_any_element(),
                                            }),
                                    ),
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
        let catalog = self.provider_catalog();
        provider_catalog_label(&catalog, &self.provider)
    }

    fn model_label(&self) -> String {
        composer_model_label(
            &self.provider,
            &self.state.current_model,
            &self.state.models,
        )
    }
}
