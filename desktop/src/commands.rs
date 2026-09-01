use serde_json::{Map, Number, Value, json};

use crate::snow::PermissionDecision;

const COMMANDS: &[(&str, &str)] = &[
    ("/agent", "Inspect subagents or configure concurrency"),
    (
        "/allow",
        "Allow a pending request once, for session, or always",
    ),
    ("/attach", "Attach an image file to the next prompt"),
    ("/attachments", "List pending image attachments"),
    ("/compact", "Compact older provider context"),
    ("/context", "Show provider-context composition"),
    ("/debug", "Inspect or configure diagnostics"),
    ("/default", "Switch to Default collaboration mode"),
    ("/deny", "Deny the pending permission request"),
    ("/detach", "Remove a pending image attachment"),
    ("/fork", "Fork the current conversation"),
    ("/goal", "Inspect or manage the thread goal"),
    ("/help", "Show commands and desktop shortcuts"),
    ("/init", "Create project guidance with Snow"),
    ("/keybindings", "Show desktop keyboard shortcuts"),
    ("/login", "Authenticate a provider"),
    ("/logout", "Remove a stored provider credential"),
    ("/mcp", "Inspect MCP servers"),
    ("/model", "Choose or set a model"),
    ("/new", "Create a new session"),
    ("/permissions", "Inspect or change permission mode"),
    ("/paste-image", "Attach an image from the clipboard"),
    ("/plan", "Switch to Plan mode or ask in Plan mode"),
    ("/processes", "Inspect managed background processes"),
    ("/quit", "Quit Snow Desktop"),
    ("/resume", "Open a stored session by RPC session ID"),
    ("/sessions", "Browse project sessions"),
    ("/settings", "Inspect or change Snow settings"),
    ("/skills", "Inspect or clear Agent Skills"),
    ("/thinking", "Choose or set thinking effort"),
    ("/tree", "Inspect and manage session branches"),
    ("/trust", "Inspect or set project trust"),
    ("/usage", "Show token and cost usage"),
];

/// How command completion should handle an exact catalog selection.
///
/// `Immediate` commands reject arguments, so the composer can submit them
/// directly instead of inserting a trailing space. Commands with optional or
/// required arguments remain editable.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CommandCompletion {
    Immediate,
    Editable,
}

const IMMEDIATE_COMMANDS: &[&str] = &[
    "/attachments",
    "/compact",
    "/context",
    "/default",
    "/deny",
    "/help",
    "/init",
    "/keybindings",
    "/mcp",
    "/new",
    "/paste-image",
    "/quit",
    "/sessions",
    "/settings",
    "/tree",
    "/usage",
];

#[derive(Debug, Clone, PartialEq)]
pub struct RpcCommand {
    pub name: String,
    pub fields: Map<String, Value>,
    pub refresh_runtime: bool,
}

impl RpcCommand {
    fn new(name: impl Into<String>) -> Self {
        Self {
            name: name.into(),
            fields: Map::new(),
            refresh_runtime: false,
        }
    }

    fn params(name: impl Into<String>, params: Value) -> Self {
        let mut command = Self::new(name);
        command.fields.insert("params".into(), params);
        command
    }

    fn field(mut self, name: &str, value: impl Into<Value>) -> Self {
        self.fields.insert(name.into(), value.into());
        self
    }

    fn refresh(mut self) -> Self {
        self.refresh_runtime = true;
        self
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum LocalCommand {
    Help,
    Keybindings,
    OpenModelPicker,
    OpenThinkingPicker,
    OpenSettings,
    OpenSessions,
    OpenBranches,
    OpenForkChooser,
    OpenPermissions,
    OpenSubagents(Option<String>),
    OpenProcesses(Option<String>),
    OpenLogin(Option<String>),
    OpenLoginProfile {
        provider: String,
        profile_name: String,
    },
    OpenLogout(Option<String>),
    AttachFile(String),
    ListAttachments,
    RemoveAttachment(Option<usize>),
    PasteImage,
    ResolvePermission(PermissionDecision),
    Quit,
}

#[derive(Debug, Clone, PartialEq)]
pub enum CommandAction {
    Local(LocalCommand),
    Rpc(RpcCommand),
    Prompt {
        message: String,
        mode: Option<String>,
    },
}

pub fn command_catalog() -> &'static [(&'static str, &'static str)] {
    COMMANDS
}

pub fn command_completion(command: &str) -> CommandCompletion {
    if IMMEDIATE_COMMANDS.contains(&command) {
        CommandCompletion::Immediate
    } else {
        CommandCompletion::Editable
    }
}

pub fn help_text() -> String {
    let mut help = String::from("Snow Desktop commands\n\n");
    for (command, description) in COMMANDS {
        help.push_str(&format!("{command:<14} {description}\n"));
    }
    help.push_str(
        "\nEnter sends · Option+Enter inserts a newline · Escape closes a picker · Stop aborts the active turn",
    );
    help
}

pub fn parse_command(line: &str) -> Result<Option<CommandAction>, String> {
    let line = line.trim();
    if !line.starts_with('/') {
        return Ok(None);
    }
    let (command, rest) = line
        .split_once(char::is_whitespace)
        .map_or((line, ""), |(command, rest)| (command, rest.trim()));

    let action = match command {
        "/q" | "/quit" => no_args(command, rest, LocalCommand::Quit)?,
        "/help" => no_args(command, rest, LocalCommand::Help)?,
        "/keybindings" => no_args(command, rest, LocalCommand::Keybindings)?,
        "/attach" => {
            let path = parse_required_single(rest, "usage: /attach <image-path>", 4_096)?;
            CommandAction::Local(LocalCommand::AttachFile(path))
        }
        "/attachments" => no_args(command, rest, LocalCommand::ListAttachments)?,
        "/paste-image" => no_args(command, rest, LocalCommand::PasteImage)?,
        "/detach" if rest.is_empty() || rest == "all" => {
            CommandAction::Local(LocalCommand::RemoveAttachment(None))
        }
        "/detach" => {
            let index = rest
                .parse::<usize>()
                .ok()
                .filter(|index| *index > 0)
                .ok_or_else(|| "usage: /detach [all|image-number]".to_owned())?;
            CommandAction::Local(LocalCommand::RemoveAttachment(Some(index - 1)))
        }
        "/model" if rest.is_empty() => CommandAction::Local(LocalCommand::OpenModelPicker),
        "/model" => {
            let model = parse_required_single(rest, "usage: /model [model-id]", 256)?;
            CommandAction::Rpc(RpcCommand::new("set_model").field("model", model).refresh())
        }
        "/thinking" if rest.is_empty() => CommandAction::Local(LocalCommand::OpenThinkingPicker),
        "/thinking" => {
            let thinking = parse_required_single(rest, "usage: /thinking [effort]", 64)?;
            CommandAction::Rpc(
                RpcCommand::new("set_thinking")
                    .field("thinking", thinking)
                    .refresh(),
            )
        }
        "/plan" if rest.is_empty() => {
            CommandAction::Rpc(RpcCommand::new("set_mode").field("mode", "plan").refresh())
        }
        "/plan" => CommandAction::Prompt {
            message: rest.into(),
            mode: Some("plan".into()),
        },
        "/default" => {
            require_empty(command, rest)?;
            CommandAction::Rpc(
                RpcCommand::new("set_mode")
                    .field("mode", "default")
                    .refresh(),
            )
        }
        "/compact" => {
            require_empty(command, rest)?;
            CommandAction::Rpc(RpcCommand::new("compact"))
        }
        "/context" => {
            require_empty(command, rest)?;
            CommandAction::Rpc(RpcCommand::new("context"))
        }
        "/usage" => {
            require_empty(command, rest)?;
            CommandAction::Rpc(RpcCommand::new("usage"))
        }
        "/goal" => {
            let goal = parse_goal(rest)?;
            CommandAction::Rpc(if goal.name == "goal_get" {
                goal
            } else {
                goal.refresh()
            })
        }
        "/debug" => CommandAction::Rpc(parse_debug(rest)?),
        "/mcp" => {
            require_empty(command, rest)?;
            CommandAction::Rpc(RpcCommand::new("mcp_servers"))
        }
        "/skills" if rest.is_empty() => CommandAction::Rpc(RpcCommand::new("skills")),
        "/skills" if rest == "clear" => CommandAction::Rpc(RpcCommand::new("skills_clear")),
        "/skills" => return Err("usage: /skills [clear]".into()),
        "/tree" => {
            require_empty(command, rest)?;
            CommandAction::Local(LocalCommand::OpenBranches)
        }
        "/fork" if rest.is_empty() => CommandAction::Local(LocalCommand::OpenForkChooser),
        "/fork" if rest == "worktree" => {
            CommandAction::Rpc(RpcCommand::params("session_worktree_fork", json!({})).refresh())
        }
        "/fork" => return Err("usage: /fork [worktree]".into()),
        "/new" => {
            require_empty(command, rest)?;
            CommandAction::Rpc(RpcCommand::params("session_create", json!({})).refresh())
        }
        "/resume" => parse_resume(rest)?,
        "/sessions" => {
            require_empty(command, rest)?;
            CommandAction::Local(LocalCommand::OpenSessions)
        }
        "/agent" => parse_agent(rest)?,
        "/processes" => CommandAction::Local(LocalCommand::OpenProcesses(parse_optional_single(
            rest,
            "usage: /processes [process-id]",
            256,
        )?)),
        "/settings" => {
            require_empty(command, rest)?;
            CommandAction::Local(LocalCommand::OpenSettings)
        }
        "/permissions" if rest.is_empty() => CommandAction::Local(LocalCommand::OpenPermissions),
        "/permissions" if matches!(rest, "ask" | "allow" | "deny") => CommandAction::Rpc(
            RpcCommand::params("permission_mode_set", json!({"mode": rest})).refresh(),
        ),
        "/permissions" => return Err("usage: /permissions [ask|allow|deny]".into()),
        "/trust" if rest.is_empty() => CommandAction::Rpc(RpcCommand::new("trust_get")),
        "/trust" if matches!(rest, "allow" | "deny") => {
            CommandAction::Rpc(RpcCommand::params("trust_set", json!({"level": rest})))
        }
        "/trust" => return Err("usage: /trust [allow|deny]".into()),
        "/init" => {
            require_empty(command, rest)?;
            CommandAction::Rpc(RpcCommand::new("project_init"))
        }
        "/login" => parse_login(rest)?,
        "/logout" => CommandAction::Local(LocalCommand::OpenLogout(parse_optional_single(
            rest,
            "usage: /logout [provider]",
            256,
        )?)),
        "/allow" => parse_allow(rest)?,
        "/deny" => {
            require_empty(command, rest)?;
            CommandAction::Local(LocalCommand::ResolvePermission(PermissionDecision::Deny))
        }
        _ => {
            return Err(format!(
                "unknown command: {command}. Use /help for commands."
            ));
        }
    };
    Ok(Some(action))
}

fn no_args(command: &str, rest: &str, action: LocalCommand) -> Result<CommandAction, String> {
    require_empty(command, rest)?;
    Ok(CommandAction::Local(action))
}

fn require_empty(command: &str, rest: &str) -> Result<(), String> {
    if rest.is_empty() {
        Ok(())
    } else {
        Err(format!("{command} takes no arguments"))
    }
}

fn parse_words(input: &str) -> Result<Vec<String>, ()> {
    let mut words = Vec::new();
    let mut word = String::new();
    let mut quote = None;
    let mut escaped = false;
    let mut started = false;

    for character in input.chars() {
        if escaped {
            word.push(character);
            started = true;
            escaped = false;
            continue;
        }
        match quote {
            Some('\'') => {
                if character == '\'' {
                    quote = None;
                } else {
                    word.push(character);
                }
            }
            Some('"') => match character {
                '"' => quote = None,
                '\\' => escaped = true,
                _ => word.push(character),
            },
            Some(_) => unreachable!("only single and double quotes are recorded"),
            None => match character {
                '\'' | '"' => {
                    quote = Some(character);
                    started = true;
                }
                '\\' => {
                    escaped = true;
                    started = true;
                }
                character if character.is_whitespace() => {
                    if started {
                        words.push(std::mem::take(&mut word));
                        started = false;
                    }
                }
                _ => {
                    word.push(character);
                    started = true;
                }
            },
        }
    }
    if quote.is_some() || escaped {
        return Err(());
    }
    if started {
        words.push(word);
    }
    Ok(words)
}

fn validate_public_argument(argument: &str, max_chars: usize) -> bool {
    !argument.is_empty()
        && argument.chars().count() <= max_chars
        && !argument.chars().any(char::is_control)
}

fn parse_optional_single(
    rest: &str,
    usage: &'static str,
    max_chars: usize,
) -> Result<Option<String>, String> {
    let arguments = parse_words(rest).map_err(|()| usage.to_owned())?;
    match arguments.as_slice() {
        [] => Ok(None),
        [argument] if validate_public_argument(argument, max_chars) => Ok(Some(argument.clone())),
        _ => Err(usage.into()),
    }
}

fn parse_required_single(
    rest: &str,
    usage: &'static str,
    max_chars: usize,
) -> Result<String, String> {
    parse_optional_single(rest, usage, max_chars)?.ok_or_else(|| usage.to_owned())
}

fn parse_login(rest: &str) -> Result<CommandAction, String> {
    const USAGE: &str = "usage: /login [provider] [profile-name]";
    let arguments = parse_words(rest).map_err(|()| USAGE.to_owned())?;
    if arguments
        .iter()
        .any(|argument| !validate_public_argument(argument, 256))
    {
        return Err(USAGE.into());
    }
    match arguments.as_slice() {
        [] => Ok(CommandAction::Local(LocalCommand::OpenLogin(None))),
        [provider] => Ok(CommandAction::Local(LocalCommand::OpenLogin(Some(
            provider.clone(),
        )))),
        [provider, profile_name] => Ok(CommandAction::Local(LocalCommand::OpenLoginProfile {
            provider: provider.clone(),
            profile_name: profile_name.clone(),
        })),
        _ => Err(USAGE.into()),
    }
}

fn parse_allow(rest: &str) -> Result<CommandAction, String> {
    let decision = match rest {
        "" | "once" => PermissionDecision::Allow,
        "session" => PermissionDecision::AllowSession,
        "always" => PermissionDecision::AllowAlways,
        _ => return Err("usage: /allow [once|session|always]".into()),
    };
    Ok(CommandAction::Local(LocalCommand::ResolvePermission(
        decision,
    )))
}

fn parse_agent(rest: &str) -> Result<CommandAction, String> {
    let args = rest.split_whitespace().collect::<Vec<_>>();
    match args.as_slice() {
        [] => Ok(CommandAction::Local(LocalCommand::OpenSubagents(None))),
        ["concurrency"] => Ok(CommandAction::Rpc(RpcCommand::new("settings_get"))),
        ["concurrency", limit] => {
            let limit = limit
                .parse::<i64>()
                .ok()
                .filter(|limit| *limit > 0)
                .ok_or_else(|| "agent concurrency must be a positive integer".to_owned())?;
            Ok(CommandAction::Rpc(RpcCommand::params(
                "settings_update",
                json!({"subagents_max_concurrent": limit}),
            )))
        }
        [path] => Ok(CommandAction::Local(LocalCommand::OpenSubagents(Some(
            (*path).to_owned(),
        )))),
        _ => Err("usage: /agent [path] | /agent concurrency [positive-integer]".into()),
    }
}

fn parse_resume(rest: &str) -> Result<CommandAction, String> {
    if rest.is_empty() {
        return Ok(CommandAction::Local(LocalCommand::OpenSessions));
    }
    if rest.chars().count() > 256
        || rest.chars().any(char::is_control)
        || rest.split_whitespace().count() != 1
    {
        return Err("usage: /resume [session-id] (use /sessions to browse)".into());
    }
    Ok(CommandAction::Rpc(
        RpcCommand::params("session_open", json!({"session_id": rest})).refresh(),
    ))
}

fn parse_goal(rest: &str) -> Result<RpcCommand, String> {
    if rest.is_empty() {
        return Ok(RpcCommand::new("goal_get"));
    }
    if matches!(rest, "pause" | "resume" | "clear" | "continue") {
        return Ok(RpcCommand::new(format!("goal_{rest}")));
    }
    if let Some(objective) = rest.strip_prefix("edit ").map(str::trim) {
        if objective.is_empty() {
            return Err("usage: /goal edit <objective>".into());
        }
        return Ok(RpcCommand::params(
            "goal_edit",
            json!({"objective": objective}),
        ));
    }
    if let Some(objective) = rest.strip_prefix("replace ").map(str::trim) {
        if objective.is_empty() {
            return Err("usage: /goal replace <objective>".into());
        }
        return Ok(RpcCommand::params(
            "goal_create",
            json!({"objective": objective, "replace": true}),
        ));
    }
    if let Some(budgeted) = rest.strip_prefix("--budget ") {
        let (budget, objective) = budgeted
            .split_once(char::is_whitespace)
            .ok_or_else(|| "usage: /goal --budget <tokens> <objective>".to_owned())?;
        let budget = budget
            .parse::<i64>()
            .ok()
            .filter(|budget| *budget > 0)
            .ok_or_else(|| "goal budget must be a positive integer".to_owned())?;
        let objective = objective.trim();
        if objective.is_empty() {
            return Err("usage: /goal --budget <tokens> <objective>".into());
        }
        return Ok(RpcCommand::params(
            "goal_create",
            Value::Object(Map::from_iter([
                ("objective".into(), Value::String(objective.into())),
                ("token_budget".into(), Value::Number(Number::from(budget))),
            ])),
        ));
    }
    Ok(RpcCommand::params(
        "goal_create",
        json!({"objective": rest}),
    ))
}

fn parse_debug(rest: &str) -> Result<RpcCommand, String> {
    const USAGE: &str = "usage: /debug [status|on|off|clear|dump [path]]";
    let arguments = parse_words(rest).map_err(|()| USAGE.to_owned())?;
    match arguments.as_slice() {
        [] => Ok(RpcCommand::new("debug_status")),
        [status] if status == "status" => Ok(RpcCommand::new("debug_status")),
        [on] if on == "on" => Ok(RpcCommand::new("debug_enable")),
        [off] if off == "off" => Ok(RpcCommand::new("debug_disable")),
        [clear] if clear == "clear" => Ok(RpcCommand::new("debug_clear")),
        [dump] if dump == "dump" => Ok(RpcCommand::params("debug_dump", json!({}))),
        [dump, path] if dump == "dump" && validate_public_argument(path, 4_096) => {
            Ok(RpcCommand::params("debug_dump", json!({"path": path})))
        }
        _ => Err(USAGE.into()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ordinary_text_is_not_a_command() {
        assert_eq!(parse_command("hello").unwrap(), None);
    }

    #[test]
    fn attachment_commands_remain_local_and_index_safely() {
        assert_eq!(
            parse_command("/attach /tmp/image.png").unwrap(),
            Some(CommandAction::Local(LocalCommand::AttachFile(
                "/tmp/image.png".into()
            )))
        );
        assert_eq!(
            parse_command("/detach 2").unwrap(),
            Some(CommandAction::Local(LocalCommand::RemoveAttachment(Some(
                1
            ))))
        );
        assert_eq!(
            parse_command("/detach all").unwrap(),
            Some(CommandAction::Local(LocalCommand::RemoveAttachment(None)))
        );
        assert!(parse_command("/detach 0").is_err());
        assert!(parse_command("/attach").is_err());
    }

    #[test]
    fn plan_with_text_is_an_atomic_mode_prompt() {
        assert_eq!(
            parse_command("/plan inspect only").unwrap(),
            Some(CommandAction::Prompt {
                message: "inspect only".into(),
                mode: Some("plan".into()),
            })
        );
    }

    #[test]
    fn goal_budget_is_validated_and_encoded() {
        let Some(CommandAction::Rpc(command)) =
            parse_command("/goal --budget 1200 finish parity").unwrap()
        else {
            panic!("expected RPC command");
        };
        assert_eq!(command.name, "goal_create");
        assert_eq!(command.fields["params"]["token_budget"], 1200);
        assert_eq!(command.fields["params"]["objective"], "finish parity");
        assert!(parse_command("/goal --budget nope work").is_err());
    }

    #[test]
    fn mode_and_runtime_mutations_request_refresh() {
        for source in ["/plan", "/default", "/model fake-2", "/thinking high"] {
            let Some(CommandAction::Rpc(command)) = parse_command(source).unwrap() else {
                panic!("expected RPC command for {source}");
            };
            assert!(command.refresh_runtime, "{source}");
        }
    }

    #[test]
    fn permission_commands_preserve_decision_scope() {
        for (source, decision) in [
            ("/allow", PermissionDecision::Allow),
            ("/allow once", PermissionDecision::Allow),
            ("/allow session", PermissionDecision::AllowSession),
            ("/allow always", PermissionDecision::AllowAlways),
            ("/deny", PermissionDecision::Deny),
        ] {
            assert_eq!(
                parse_command(source).unwrap(),
                Some(CommandAction::Local(LocalCommand::ResolvePermission(
                    decision
                ))),
                "{source}"
            );
        }
        assert!(parse_command("/allow forever").is_err());
        assert!(parse_command("/deny always").is_err());
    }

    #[test]
    fn trust_uses_the_rpc_level_field() {
        for level in ["allow", "deny"] {
            let Some(CommandAction::Rpc(command)) =
                parse_command(&format!("/trust {level}")).unwrap()
            else {
                panic!("expected trust RPC command");
            };
            assert_eq!(command.name, "trust_set");
            assert_eq!(command.fields["params"]["level"], level);
            assert!(command.fields["params"].get("decision").is_none());
        }
    }

    #[test]
    fn agent_concurrency_uses_settings_rpc_without_losing_fleet_filter() {
        let Some(CommandAction::Rpc(get)) = parse_command("/agent concurrency").unwrap() else {
            panic!("expected settings_get");
        };
        assert_eq!(get.name, "settings_get");

        let Some(CommandAction::Rpc(update)) = parse_command("/agent concurrency 7").unwrap()
        else {
            panic!("expected settings_update");
        };
        assert_eq!(update.name, "settings_update");
        assert_eq!(update.fields["params"]["subagents_max_concurrent"], 7);
        assert_eq!(
            parse_command("/agent child/worker").unwrap(),
            Some(CommandAction::Local(LocalCommand::OpenSubagents(Some(
                "child/worker".into()
            ))))
        );
        assert!(parse_command("/agent concurrency 0").is_err());
        assert!(parse_command("/agent concurrency seven").is_err());
        assert!(parse_command("/agent child extra").is_err());
    }

    #[test]
    fn resume_accepts_only_one_bounded_rpc_session_id() {
        assert_eq!(
            parse_command("/resume").unwrap(),
            Some(CommandAction::Local(LocalCommand::OpenSessions))
        );
        let Some(CommandAction::Rpc(open)) = parse_command("/resume session-123").unwrap() else {
            panic!("expected session_open");
        };
        assert_eq!(open.name, "session_open");
        assert_eq!(open.fields["params"]["session_id"], "session-123");
        assert!(open.refresh_runtime);
        assert!(parse_command("/resume one two").is_err());
        assert!(parse_command(&format!("/resume {}", "x".repeat(257))).is_err());
    }

    #[test]
    fn context_uses_the_canonical_context_rpc_without_payload() {
        for source in ["/context", "  /context  ", "/context\t"] {
            assert_eq!(
                parse_command(source).unwrap(),
                Some(CommandAction::Rpc(RpcCommand::new("context"))),
                "unexpected action for {source:?}",
            );
        }

        let Some(CommandAction::Rpc(command)) = parse_command("/context").unwrap() else {
            panic!("expected context RPC command");
        };
        assert_eq!(command.name, "context");
        assert!(command.fields.is_empty(), "context must not send params");
        assert!(!command.refresh_runtime);

        for source in ["/context now", "/context clear", "/context --json"] {
            assert_eq!(
                parse_command(source),
                Err("/context takes no arguments".into()),
                "unexpected validation for {source:?}",
            );
        }
    }

    #[test]
    fn skills_list_and_clear_use_distinct_canonical_rpcs() {
        for source in ["/skills", "  /skills  ", "/skills\t"] {
            let Some(CommandAction::Rpc(command)) = parse_command(source).unwrap() else {
                panic!("expected skills RPC command for {source:?}");
            };
            assert_eq!(command.name, "skills");
            assert!(command.fields.is_empty(), "skills must not send params");
            assert!(!command.refresh_runtime);
        }

        for source in ["/skills clear", " /skills   clear ", "/skills\tclear"] {
            let Some(CommandAction::Rpc(command)) = parse_command(source).unwrap() else {
                panic!("expected skills_clear RPC command for {source:?}");
            };
            assert_eq!(command.name, "skills_clear");
            assert!(
                command.fields.is_empty(),
                "skills_clear must not send params"
            );
            assert!(!command.refresh_runtime);
        }

        for source in [
            "/skills list",
            "/skills Clear",
            "/skills clear now",
            "/skills --clear",
        ] {
            assert_eq!(
                parse_command(source),
                Err("usage: /skills [clear]".into()),
                "unexpected validation for {source:?}",
            );
        }
    }

    #[test]
    fn sensitive_login_data_is_not_parsed_as_rpc_fields() {
        assert_eq!(
            parse_command("/login chatgpt").unwrap(),
            Some(CommandAction::Local(LocalCommand::OpenLogin(Some(
                "chatgpt".into()
            ))))
        );
    }

    #[test]
    fn debug_dump_parses_exactly_one_quote_aware_path() {
        for (source, path) in [
            (
                r#"/debug dump "/tmp/snow dump.json""#,
                "/tmp/snow dump.json",
            ),
            ("/debug dump '/tmp/other dump.json'", "/tmp/other dump.json"),
            (
                r"/debug dump /tmp/escaped\ path.json",
                "/tmp/escaped path.json",
            ),
        ] {
            let Some(CommandAction::Rpc(command)) = parse_command(source).unwrap() else {
                panic!("expected debug_dump for {source:?}");
            };
            assert_eq!(command.name, "debug_dump");
            assert_eq!(command.fields["params"]["path"], path);
        }

        for source in [
            "/debug dump /tmp/two words.json",
            "/debug dump one two",
            "/debug dump \"unterminated",
            "/debug dump ''",
        ] {
            assert_eq!(
                parse_command(source),
                Err("usage: /debug [status|on|off|clear|dump [path]]".into()),
                "unexpected validation for {source:?}",
            );
        }
    }

    #[test]
    fn one_target_commands_reject_ambiguous_extra_arguments() {
        assert_eq!(
            parse_command("/processes child-1").unwrap(),
            Some(CommandAction::Local(LocalCommand::OpenProcesses(Some(
                "child-1".into(),
            )))),
        );
        assert_eq!(
            parse_command("/logout opencode-go").unwrap(),
            Some(CommandAction::Local(LocalCommand::OpenLogout(Some(
                "opencode-go".into(),
            )))),
        );
        for source in [
            "/processes child-1 extra",
            "/logout opencode-go extra",
            "/model fake-model extra",
            "/thinking high extra",
            "/attach one.png extra",
        ] {
            assert!(parse_command(source).is_err(), "{source} must be rejected");
        }
        assert!(parse_command("/quit now").is_err());
        assert_eq!(
            parse_command(r#"/attach "image with spaces.png""#).unwrap(),
            Some(CommandAction::Local(LocalCommand::AttachFile(
                "image with spaces.png".into(),
            ))),
        );
    }

    #[test]
    fn login_accepts_only_public_provider_and_profile_identifiers() {
        assert_eq!(
            parse_command("/login").unwrap(),
            Some(CommandAction::Local(LocalCommand::OpenLogin(None))),
        );
        assert_eq!(
            parse_command("/login chatgpt").unwrap(),
            Some(CommandAction::Local(LocalCommand::OpenLogin(Some(
                "chatgpt".into(),
            )))),
        );
        assert_eq!(
            parse_command(r#"/login openai-compatible "work profile""#).unwrap(),
            Some(CommandAction::Local(LocalCommand::OpenLoginProfile {
                provider: "openai-compatible".into(),
                profile_name: "work profile".into(),
            })),
        );
        for source in [
            "/login provider profile secret",
            "/login ''",
            "/login provider ''",
            "/login \"unterminated",
        ] {
            assert_eq!(
                parse_command(source),
                Err("usage: /login [provider] [profile-name]".into()),
            );
        }
        assert!(parse_command(&format!("/login {}", "x".repeat(257))).is_err());
    }

    #[test]
    fn bare_fork_and_permissions_open_explicit_choosers() {
        assert_eq!(
            parse_command("/fork").unwrap(),
            Some(CommandAction::Local(LocalCommand::OpenForkChooser)),
        );
        assert_eq!(
            parse_command("/permissions").unwrap(),
            Some(CommandAction::Local(LocalCommand::OpenPermissions)),
        );

        let Some(CommandAction::Rpc(worktree)) = parse_command("/fork worktree").unwrap() else {
            panic!("expected explicit worktree fork RPC");
        };
        assert_eq!(worktree.name, "session_worktree_fork");
        for mode in ["ask", "allow", "deny"] {
            let Some(CommandAction::Rpc(command)) =
                parse_command(&format!("/permissions {mode}")).unwrap()
            else {
                panic!("expected permission mode RPC");
            };
            assert_eq!(command.name, "permission_mode_set");
            assert_eq!(command.fields["params"]["mode"], mode);
        }
    }

    #[test]
    fn completion_metadata_marks_only_no_argument_commands_immediate() {
        for command in IMMEDIATE_COMMANDS {
            assert_eq!(
                command_completion(command),
                CommandCompletion::Immediate,
                "{command}",
            );
            assert!(
                parse_command(&format!("{command} unexpected")).is_err(),
                "immediate command {command} must reject arguments",
            );
        }
        for command in [
            "/agent",
            "/allow",
            "/attach",
            "/debug",
            "/fork",
            "/login",
            "/logout",
            "/model",
            "/permissions",
            "/processes",
            "/thinking",
        ] {
            assert_eq!(
                command_completion(command),
                CommandCompletion::Editable,
                "{command}",
            );
        }
    }

    #[test]
    fn catalog_contains_every_dispatched_command() {
        let catalog = COMMANDS.iter().map(|entry| entry.0).collect::<Vec<_>>();
        for command in [
            "/agent",
            "/allow",
            "/compact",
            "/context",
            "/debug",
            "/default",
            "/deny",
            "/fork",
            "/goal",
            "/help",
            "/init",
            "/keybindings",
            "/login",
            "/logout",
            "/mcp",
            "/model",
            "/new",
            "/permissions",
            "/plan",
            "/processes",
            "/quit",
            "/resume",
            "/sessions",
            "/settings",
            "/skills",
            "/thinking",
            "/tree",
            "/trust",
            "/usage",
        ] {
            assert!(catalog.contains(&command), "missing {command}");
        }
    }
}
