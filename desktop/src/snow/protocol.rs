use std::collections::HashSet;

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};

use super::SnowError;

pub const RPC_PROTOCOL_VERSION: &str = "1";
pub const DEFAULT_MAX_FRAME_BYTES: usize = 16 * 1024 * 1024;
pub const REQUIRED_CAPABILITIES: [&str; 7] = [
    "prompt_completion",
    "session_info",
    "messages_list",
    "models_list",
    "branch_management",
    "permission_interaction",
    "user_input",
];

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum RpcRequest {
    Prompt {
        id: String,
        message: String,
    },
    Abort {
        id: String,
    },
    ModelsList {
        id: String,
    },
    SessionInfo {
        id: String,
    },
    MessagesList {
        id: String,
    },
    BranchesList {
        id: String,
    },
    BranchSelect {
        id: String,
        params: BranchSelectParams,
    },
    BranchFork {
        id: String,
        params: BranchForkParams,
    },
    SessionRename {
        id: String,
        params: SessionRenameParams,
    },
    SetModel {
        id: String,
        model: String,
        #[serde(skip_serializing_if = "Option::is_none")]
        thinking: Option<String>,
    },
    SetThinking {
        id: String,
        thinking: String,
    },
    PermissionReply {
        id: String,
        params: PermissionReplyParams,
    },
    PermissionReject {
        id: String,
        params: PermissionRejectParams,
    },
    UserInputReply {
        id: String,
        params: UserInputReplyParams,
    },
    UserInputReject {
        id: String,
        params: UserInputRejectParams,
    },
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct BranchSelectParams {
    pub branch_id: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct BranchForkParams {
    #[serde(skip_serializing_if = "String::is_empty")]
    pub source_branch_id: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct SessionRenameParams {
    pub name: String,
}

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum PermissionDecision {
    Allow,
    AllowSession,
    AllowAlways,
    Deny,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct PermissionReplyParams {
    pub request_id: String,
    pub decision: PermissionDecision,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct PermissionRejectParams {
    pub request_id: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct UserInputRejectParams {
    pub request_id: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct UserInputReplyParams {
    pub request_id: String,
    pub answers: Vec<UserInputAnswer>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct UserInputAnswer {
    #[serde(rename = "id")]
    pub question_id: String,
    pub answer: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct PermissionRequest {
    pub id: String,
    pub tool: String,
    pub args: Value,
    #[serde(default)]
    pub paths: Vec<String>,
    pub risk: String,
    #[serde(default)]
    pub reason: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct UserInputOption {
    pub label: String,
    #[serde(default)]
    pub description: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct UserInputQuestion {
    pub id: String,
    pub header: String,
    pub question: String,
    #[serde(default)]
    pub options: Vec<UserInputOption>,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct UserInputRequest {
    pub id: String,
    pub tool_call_id: String,
    pub questions: Vec<UserInputQuestion>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum InteractionKind {
    Permission,
    UserInput,
}

impl InteractionKind {
    pub const fn label(self) -> &'static str {
        match self {
            Self::Permission => "permission",
            Self::UserInput => "user input",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MalformedInteraction {
    pub kind: InteractionKind,
    pub request_id: Option<String>,
    pub error: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct SessionBranch {
    pub id: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub parent_branch_id: String,
    #[serde(default)]
    pub forked_from_id: String,
    pub tip_id: String,
    pub messages: usize,
    #[serde(default)]
    pub preview: String,
    pub created_at: i64,
    pub updated_at: i64,
    pub active: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct BranchCatalog {
    pub branches: Vec<SessionBranch>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct SessionRenameResult {
    pub session_id: String,
    pub name: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct ModelInfo {
    pub provider: String,
    pub id: String,
    #[serde(default)]
    pub display_name: String,
    #[serde(default)]
    pub supports_thinking: bool,
    #[serde(default)]
    pub default_thinking: String,
    #[serde(default)]
    pub thinking_levels: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct ModelCatalog {
    #[serde(default)]
    pub provider: String,
    #[serde(default)]
    pub current: String,
    #[serde(default)]
    pub models: Vec<ModelInfo>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct SessionInfo {
    pub session_id: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub path: String,
    #[serde(default)]
    pub cwd: String,
    pub provider: String,
    pub model: String,
    #[serde(default = "default_thinking_level")]
    pub thinking: String,
    #[serde(default)]
    pub thinking_levels: Vec<String>,
}

fn default_thinking_level() -> String {
    "off".into()
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HistoryMessage {
    pub role: String,
    pub text: String,
}

#[derive(Debug, Deserialize)]
struct HistoryData {
    #[serde(default)]
    messages: Vec<HistoryMessageWire>,
}

#[derive(Debug, Deserialize)]
struct HistoryMessageWire {
    role: String,
    #[serde(default)]
    content: Vec<HistoryContentWire>,
}

#[derive(Debug, Deserialize)]
struct HistoryContentWire {
    #[serde(rename = "type")]
    kind: String,
    #[serde(default)]
    text: String,
}

pub fn decode_history(value: Value) -> Result<Vec<HistoryMessage>, SnowError> {
    let history: HistoryData = serde_json::from_value(value)
        .map_err(|error| SnowError::Protocol(format!("invalid messages_list data: {error}")))?;
    Ok(history
        .messages
        .into_iter()
        .filter_map(|message| {
            if !matches!(message.role.as_str(), "user" | "assistant") {
                return None;
            }
            let text = message
                .content
                .into_iter()
                .filter(|block| matches!(block.kind.as_str(), "text" | "plan"))
                .map(|block| block.text)
                .filter(|text| !text.is_empty())
                .collect::<Vec<_>>()
                .join("\n");
            (!text.is_empty()).then_some(HistoryMessage {
                role: message.role,
                text,
            })
        })
        .collect())
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RpcReady {
    pub protocol_version: String,
    pub snow_version: String,
    pub capabilities: HashSet<String>,
    pub max_input_bytes: usize,
}

#[derive(Debug, Clone, PartialEq)]
pub struct RpcResponse {
    pub id: Option<String>,
    pub command: Option<String>,
    pub success: bool,
    pub data: Option<Value>,
    pub error: Option<String>,
    pub error_code: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PromptStatus {
    Completed,
    Failed,
    Canceled,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PromptCompleted {
    pub request_id: String,
    pub status: PromptStatus,
    pub error: Option<String>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct AgentEvent {
    pub kind: String,
    pub fields: Map<String, Value>,
}

impl AgentEvent {
    pub fn string(&self, field: &str) -> Option<&str> {
        self.fields.get(field)?.as_str()
    }

    pub fn boolean(&self, field: &str) -> Option<bool> {
        self.fields.get(field)?.as_bool()
    }

    pub fn has_agent(&self) -> bool {
        self.fields.contains_key("agent")
    }

    pub fn nested_string(&self, object: &str, field: &str) -> Option<&str> {
        self.fields.get(object)?.as_object()?.get(field)?.as_str()
    }

    pub fn nested_object_string(&self, outer: &str, inner: &str, field: &str) -> Option<&str> {
        self.fields
            .get(outer)?
            .as_object()?
            .get(inner)?
            .as_object()?
            .get(field)?
            .as_str()
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct RawFrame {
    pub kind: String,
    pub fields: Map<String, Value>,
}

#[derive(Debug, Clone, PartialEq)]
pub enum RpcFrame {
    Ready(RpcReady),
    Response(RpcResponse),
    PromptCompleted(PromptCompleted),
    PermissionRequest(PermissionRequest),
    UserInputRequest(UserInputRequest),
    MalformedInteraction(MalformedInteraction),
    Agent(AgentEvent),
    Unknown(RawFrame),
}

#[derive(Debug, Deserialize)]
struct ReadyWire {
    #[serde(rename = "type")]
    kind: String,
    protocol_version: String,
    snow_version: String,
    capabilities: Vec<String>,
    max_input_bytes: usize,
}

const AGENT_EVENT_TYPES: &[&str] = &[
    "session_updated",
    "run_stats_updated",
    "text_delta",
    "thinking_delta",
    "tool_start",
    "tool_progress",
    "tool_end",
    "tool_routing",
    "permission_request",
    "user_input_request",
    "usage",
    "provider_retry",
    "queue_updated",
    "turn_done",
    "error",
    "aborted",
    "model_changed",
    "mode_changed",
    "plan_started",
    "plan_delta",
    "plan_completed",
    "plan_update",
    "compaction_started",
    "compaction_done",
    "thread_goal_updated",
    "subagent_started",
    "subagent_status",
    "subagent_message",
    "subagent_activity",
];

pub fn encode_request(request: &RpcRequest, limit: usize) -> Result<Vec<u8>, SnowError> {
    let mut frame = serde_json::to_vec(request)
        .map_err(|error| SnowError::Protocol(format!("could not serialize request: {error}")))?;
    if frame.len() > limit {
        return Err(SnowError::FrameTooLarge { limit });
    }
    frame.push(b'\n');
    Ok(frame)
}

pub fn decode_frame(bytes: &[u8]) -> Result<RpcFrame, SnowError> {
    let text = std::str::from_utf8(bytes).map_err(|_| SnowError::InvalidUtf8)?;
    let value: Value =
        serde_json::from_str(text).map_err(|error| SnowError::InvalidJson(error.to_string()))?;
    let object = value
        .as_object()
        .ok_or_else(|| SnowError::Protocol("stdout frame must be a JSON object".into()))?;
    let kind = object
        .get("type")
        .and_then(Value::as_str)
        .ok_or_else(|| SnowError::Protocol("stdout frame is missing string field type".into()))?
        .to_owned();

    match kind.as_str() {
        "rpc_ready" => decode_ready(value),
        "response" => decode_response(object),
        "prompt_completed" => decode_prompt_completed(object),
        "permission_request" if !object.contains_key("agent") => decode_permission_request(object),
        "user_input_request" if !object.contains_key("agent") => decode_user_input_request(object),
        _ if AGENT_EVENT_TYPES.contains(&kind.as_str()) => {
            let mut fields = object.clone();
            fields.remove("type");
            Ok(RpcFrame::Agent(AgentEvent { kind, fields }))
        }
        _ => {
            let mut fields = object.clone();
            fields.remove("type");
            Ok(RpcFrame::Unknown(RawFrame { kind, fields }))
        }
    }
}

fn decode_permission_request(object: &Map<String, Value>) -> Result<RpcFrame, SnowError> {
    let request_id = object
        .get("permission")
        .and_then(Value::as_object)
        .and_then(|permission| permission.get("request"))
        .and_then(Value::as_object)
        .and_then(|request| request.get("id"))
        .and_then(Value::as_str)
        .filter(|id| !id.trim().is_empty())
        .map(str::to_owned);
    let result = object
        .get("permission")
        .and_then(Value::as_object)
        .and_then(|permission| permission.get("request"))
        .cloned()
        .ok_or_else(|| "permission_request is missing permission.request".to_owned())
        .and_then(|value| {
            serde_json::from_value::<PermissionRequest>(value)
                .map_err(|error| format!("invalid permission_request: {error}"))
        })
        .and_then(validate_permission_request);
    match result {
        Ok(request) => Ok(RpcFrame::PermissionRequest(request)),
        Err(error) => Ok(RpcFrame::MalformedInteraction(MalformedInteraction {
            kind: InteractionKind::Permission,
            request_id,
            error,
        })),
    }
}

fn validate_permission_request(request: PermissionRequest) -> Result<PermissionRequest, String> {
    if request.id.trim().is_empty() {
        return Err("permission_request id must be non-empty".into());
    }
    if request.tool.trim().is_empty() {
        return Err("permission_request tool must be non-empty".into());
    }
    if request.risk.trim().is_empty() {
        return Err("permission_request risk must be non-empty".into());
    }
    Ok(request)
}

fn decode_user_input_request(object: &Map<String, Value>) -> Result<RpcFrame, SnowError> {
    let request_id = object
        .get("user_input")
        .and_then(Value::as_object)
        .and_then(|request| request.get("id"))
        .and_then(Value::as_str)
        .filter(|id| !id.trim().is_empty())
        .map(str::to_owned);
    let result = object
        .get("user_input")
        .cloned()
        .ok_or_else(|| "user_input_request is missing user_input".to_owned())
        .and_then(|value| {
            serde_json::from_value::<UserInputRequest>(value)
                .map_err(|error| format!("invalid user_input_request: {error}"))
        })
        .and_then(validate_user_input_request);
    match result {
        Ok(request) => Ok(RpcFrame::UserInputRequest(request)),
        Err(error) => Ok(RpcFrame::MalformedInteraction(MalformedInteraction {
            kind: InteractionKind::UserInput,
            request_id,
            error,
        })),
    }
}

fn validate_user_input_request(request: UserInputRequest) -> Result<UserInputRequest, String> {
    if request.id.trim().is_empty() {
        return Err("user_input_request id must be non-empty".into());
    }
    if request.tool_call_id.trim().is_empty() {
        return Err("user_input_request tool_call_id must be non-empty".into());
    }
    if request.questions.is_empty() {
        return Err("user_input_request questions must be non-empty".into());
    }
    let mut ids = HashSet::with_capacity(request.questions.len());
    for question in &request.questions {
        if question.id.trim().is_empty()
            || question.header.trim().is_empty()
            || question.question.trim().is_empty()
        {
            return Err("user_input_request question fields must be non-empty".into());
        }
        if !ids.insert(question.id.as_str()) {
            return Err("user_input_request question ids must be unique".into());
        }
        if question
            .options
            .iter()
            .any(|option| option.label.trim().is_empty())
        {
            return Err("user_input_request option labels must be non-empty".into());
        }
    }
    Ok(request)
}

fn decode_ready(value: Value) -> Result<RpcFrame, SnowError> {
    let ready: ReadyWire = serde_json::from_value(value)
        .map_err(|error| SnowError::Protocol(format!("invalid rpc_ready frame: {error}")))?;
    if ready.kind != "rpc_ready" {
        return Err(SnowError::Protocol("invalid rpc_ready type".into()));
    }
    if ready.max_input_bytes == 0 {
        return Err(SnowError::Protocol(
            "rpc_ready max_input_bytes must be positive".into(),
        ));
    }
    Ok(RpcFrame::Ready(RpcReady {
        protocol_version: ready.protocol_version,
        snow_version: ready.snow_version,
        capabilities: ready.capabilities.into_iter().collect(),
        max_input_bytes: ready.max_input_bytes,
    }))
}

fn decode_response(object: &Map<String, Value>) -> Result<RpcFrame, SnowError> {
    let success = required_bool(object, "success")?;
    let error = optional_string(object, "error")?;
    if !success && error.as_deref().is_none_or(str::is_empty) {
        return Err(SnowError::Protocol(
            "failed response must contain a non-empty error".into(),
        ));
    }
    Ok(RpcFrame::Response(RpcResponse {
        id: optional_string(object, "id")?,
        command: optional_string(object, "command")?,
        success,
        data: object.get("data").cloned(),
        error,
        error_code: optional_string(object, "error_code")?,
    }))
}

fn decode_prompt_completed(object: &Map<String, Value>) -> Result<RpcFrame, SnowError> {
    let request_id = required_string(object, "request_id")?;
    let status = match required_string(object, "status")?.as_str() {
        "completed" => PromptStatus::Completed,
        "failed" => PromptStatus::Failed,
        "canceled" => PromptStatus::Canceled,
        other => {
            return Err(SnowError::Protocol(format!(
                "unknown prompt completion status {other}"
            )));
        }
    };
    let error = optional_string(object, "error")?;
    if status == PromptStatus::Failed && error.as_deref().is_none_or(str::is_empty) {
        return Err(SnowError::Protocol(
            "failed prompt completion must contain a non-empty error".into(),
        ));
    }
    Ok(RpcFrame::PromptCompleted(PromptCompleted {
        request_id,
        status,
        error,
    }))
}

fn required_string(object: &Map<String, Value>, field: &str) -> Result<String, SnowError> {
    object
        .get(field)
        .and_then(Value::as_str)
        .map(str::to_owned)
        .ok_or_else(|| SnowError::Protocol(format!("frame is missing string field {field}")))
}

fn optional_string(object: &Map<String, Value>, field: &str) -> Result<Option<String>, SnowError> {
    match object.get(field) {
        None => Ok(None),
        Some(value) => value
            .as_str()
            .map(|value| Some(value.to_owned()))
            .ok_or_else(|| SnowError::Protocol(format!("frame field {field} must be a string"))),
    }
}

fn required_bool(object: &Map<String, Value>, field: &str) -> Result<bool, SnowError> {
    object
        .get(field)
        .and_then(Value::as_bool)
        .ok_or_else(|| SnowError::Protocol(format!("frame is missing boolean field {field}")))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn request_encoding_is_jsonl() {
        let frame = encode_request(
            &RpcRequest::Prompt {
                id: "p1".into(),
                message: "hello".into(),
            },
            1024,
        )
        .unwrap();
        assert_eq!(
            frame,
            br#"{"type":"prompt","id":"p1","message":"hello"}
"#
        );
    }

    #[test]
    fn abort_encoding_is_jsonl() {
        let frame = encode_request(&RpcRequest::Abort { id: "a1".into() }, 1024).unwrap();
        assert_eq!(
            frame,
            br#"{"type":"abort","id":"a1"}
"#
        );
    }

    #[test]
    fn runtime_state_requests_are_jsonl() {
        assert_eq!(
            encode_request(&RpcRequest::ModelsList { id: "m1".into() }, 1024).unwrap(),
            b"{\"type\":\"models_list\",\"id\":\"m1\"}\n"
        );
        assert_eq!(
            encode_request(
                &RpcRequest::SetModel {
                    id: "m2".into(),
                    model: "model-two".into(),
                    thinking: Some("high".into()),
                },
                1024,
            )
            .unwrap(),
            b"{\"type\":\"set_model\",\"id\":\"m2\",\"model\":\"model-two\",\"thinking\":\"high\"}\n"
        );
        assert_eq!(
            encode_request(
                &RpcRequest::SetThinking {
                    id: "t1".into(),
                    thinking: "medium".into(),
                },
                1024,
            )
            .unwrap(),
            b"{\"type\":\"set_thinking\",\"id\":\"t1\",\"thinking\":\"medium\"}\n"
        );
        assert_eq!(
            encode_request(&RpcRequest::BranchesList { id: "b1".into() }, 1024).unwrap(),
            b"{\"type\":\"branches_list\",\"id\":\"b1\"}\n"
        );
        assert_eq!(
            encode_request(
                &RpcRequest::BranchSelect {
                    id: "b2".into(),
                    params: BranchSelectParams {
                        branch_id: "experiment".into(),
                    },
                },
                1024,
            )
            .unwrap(),
            b"{\"type\":\"branch_select\",\"id\":\"b2\",\"params\":{\"branch_id\":\"experiment\"}}\n"
        );
        assert_eq!(
            encode_request(
                &RpcRequest::BranchFork {
                    id: "b3".into(),
                    params: BranchForkParams {
                        source_branch_id: String::new(),
                    },
                },
                1024,
            )
            .unwrap(),
            b"{\"type\":\"branch_fork\",\"id\":\"b3\",\"params\":{}}\n"
        );
        assert_eq!(
            encode_request(
                &RpcRequest::SessionRename {
                    id: "r1".into(),
                    params: SessionRenameParams {
                        name: "API cleanup".into(),
                    },
                },
                1024,
            )
            .unwrap(),
            b"{\"type\":\"session_rename\",\"id\":\"r1\",\"params\":{\"name\":\"API cleanup\"}}\n"
        );
    }

    #[test]
    fn interaction_requests_have_exact_jsonl_encoding() {
        let cases = [
            (
                RpcRequest::PermissionReply {
                    id: "c1".into(),
                    params: PermissionReplyParams {
                        request_id: "perm-1".into(),
                        decision: PermissionDecision::AllowSession,
                    },
                },
                "{\"type\":\"permission_reply\",\"id\":\"c1\",\"params\":{\"request_id\":\"perm-1\",\"decision\":\"allow_session\"}}\n",
            ),
            (
                RpcRequest::PermissionReject {
                    id: "c2".into(),
                    params: PermissionRejectParams {
                        request_id: "perm-1".into(),
                    },
                },
                "{\"type\":\"permission_reject\",\"id\":\"c2\",\"params\":{\"request_id\":\"perm-1\"}}\n",
            ),
            (
                RpcRequest::UserInputReply {
                    id: "c3".into(),
                    params: UserInputReplyParams {
                        request_id: "ask-1".into(),
                        answers: vec![UserInputAnswer {
                            question_id: "language".into(),
                            answer: "Rust".into(),
                        }],
                    },
                },
                "{\"type\":\"user_input_reply\",\"id\":\"c3\",\"params\":{\"request_id\":\"ask-1\",\"answers\":[{\"id\":\"language\",\"answer\":\"Rust\"}]}}\n",
            ),
            (
                RpcRequest::UserInputReject {
                    id: "c4".into(),
                    params: UserInputRejectParams {
                        request_id: "ask-1".into(),
                    },
                },
                "{\"type\":\"user_input_reject\",\"id\":\"c4\",\"params\":{\"request_id\":\"ask-1\"}}\n",
            ),
        ];
        for (request, expected) in cases {
            assert_eq!(encode_request(&request, 1024).unwrap(), expected.as_bytes());
        }
    }

    #[test]
    fn root_interaction_events_decode_to_typed_requests() {
        let RpcFrame::PermissionRequest(permission) = decode_frame(
            br#"{"type":"permission_request","permission":{"request":{"id":"perm-1","tool":"bash","args":{"command":"pwd"},"paths":["/tmp"],"risk":"exec","reason":"run command"}}}"#,
        )
        .unwrap()
        else {
            panic!("wanted permission request")
        };
        assert_eq!(permission.id, "perm-1");
        assert_eq!(permission.args["command"], "pwd");

        let RpcFrame::UserInputRequest(user_input) = decode_frame(
            br#"{"type":"user_input_request","user_input":{"id":"ask-1","tool_call_id":"call-1","questions":[{"id":"language","header":"Language","question":"Which language?","options":[{"label":"Rust","description":"Safe"}]}]}}"#,
        )
        .unwrap()
        else {
            panic!("wanted user input request")
        };
        assert_eq!(user_input.id, "ask-1");
        assert_eq!(user_input.questions[0].options[0].label, "Rust");
    }

    #[test]
    fn malformed_interaction_preserves_usable_request_id() {
        let RpcFrame::MalformedInteraction(malformed) = decode_frame(
            br#"{"type":"permission_request","permission":{"request":{"id":"perm-1","tool":"bash","args":{},"risk":17}}}"#,
        )
        .unwrap()
        else {
            panic!("wanted malformed interaction")
        };
        assert_eq!(malformed.kind, InteractionKind::Permission);
        assert_eq!(malformed.request_id.as_deref(), Some("perm-1"));
        assert!(malformed.error.contains("invalid permission_request"));
    }

    #[test]
    fn attributed_interactions_remain_raw_agent_events() {
        let RpcFrame::Agent(event) = decode_frame(
            br#"{"type":"permission_request","permission":{"request":{"id":"child-perm","tool":"bash","args":{},"risk":"exec"}},"agent":{"id":"child-1"}}"#,
        )
        .unwrap()
        else {
            panic!("wanted attributed agent event")
        };
        assert!(event.has_agent());
    }

    #[test]
    fn session_and_model_metadata_preserve_thinking_capabilities() {
        let session: SessionInfo = serde_json::from_value(serde_json::json!({
            "session_id": "s1",
            "name": "Desktop proof",
            "path": "/tmp/session.db",
            "cwd": "/tmp/snow-core",
            "provider": "fake",
            "model": "fake-1",
            "thinking": "high",
            "thinking_levels": ["off", "low", "high"]
        }))
        .unwrap();
        assert_eq!(session.cwd, "/tmp/snow-core");
        assert_eq!(session.thinking, "high");
        assert_eq!(session.thinking_levels, ["off", "low", "high"]);

        let catalog: ModelCatalog = serde_json::from_value(serde_json::json!({
            "provider": "fake",
            "current": "fake-1",
            "models": [{
                "provider": "fake",
                "id": "fake-1",
                "supports_thinking": true,
                "default_thinking": "low",
                "thinking_levels": ["low", "high"]
            }]
        }))
        .unwrap();
        assert!(catalog.models[0].supports_thinking);
        assert_eq!(catalog.models[0].default_thinking, "low");
        assert_eq!(catalog.models[0].thinking_levels, ["low", "high"]);
    }

    #[test]
    fn branch_catalog_decodes_required_and_optional_metadata() {
        let catalog: BranchCatalog = serde_json::from_value(serde_json::json!({
            "branches": [
                {
                    "id": "main",
                    "tip_id": "entry-1",
                    "messages": 2,
                    "created_at": 10,
                    "updated_at": 11,
                    "active": false
                },
                {
                    "id": "experiment",
                    "name": "Experiment",
                    "parent_branch_id": "main",
                    "forked_from_id": "entry-1",
                    "tip_id": "entry-1",
                    "messages": 2,
                    "preview": "Try another approach",
                    "created_at": 12,
                    "updated_at": 13,
                    "active": true
                }
            ]
        }))
        .unwrap();

        assert_eq!(catalog.branches.len(), 2);
        assert_eq!(catalog.branches[0].name, "");
        assert_eq!(catalog.branches[1].parent_branch_id, "main");
        assert!(catalog.branches[1].active);
        assert!(serde_json::from_value::<BranchCatalog>(serde_json::json!({})).is_err());
    }

    #[test]
    fn history_decoder_keeps_only_surface_safe_text() {
        let history = decode_history(serde_json::json!({
            "messages": [
                {"role":"user","content":[{"type":"text","text":"question"}]},
                {"role":"assistant","content":[
                    {"type":"thinking","text":"private thought"},
                    {"type":"text","text":"answer"},
                    {"type":"provider_data","data":"opaque"}
                ]},
                {"role":"tool_result","content":[{"type":"text","text":"secret output"}]},
                {"role":"system","content":[{"type":"text","text":"system context"}]},
                {"role":"custom","content":[{"type":"text","text":"compaction checkpoint"}]}
            ]
        }))
        .unwrap();
        assert_eq!(
            history,
            vec![
                HistoryMessage {
                    role: "user".into(),
                    text: "question".into(),
                },
                HistoryMessage {
                    role: "assistant".into(),
                    text: "answer".into(),
                },
            ]
        );
    }

    #[test]
    fn request_encoding_enforces_limit() {
        let error = encode_request(
            &RpcRequest::Prompt {
                id: "p1".into(),
                message: "too long".into(),
            },
            4,
        )
        .unwrap_err();
        assert!(matches!(error, SnowError::FrameTooLarge { limit: 4 }));
    }

    #[test]
    fn decodes_ready_and_preserves_capabilities() {
        let frame = decode_frame(
            br#"{"type":"rpc_ready","protocol_version":"1","snow_version":"dev","capabilities":["prompt_completion","session_info"],"max_input_bytes":1024}"#,
        )
        .unwrap();
        let RpcFrame::Ready(ready) = frame else {
            panic!("wanted ready")
        };
        assert_eq!(ready.protocol_version, "1");
        assert!(ready.capabilities.contains("prompt_completion"));
        assert_eq!(ready.max_input_bytes, 1024);
    }

    #[test]
    fn decodes_all_prompt_completion_states() {
        for (status, expected) in [
            ("completed", PromptStatus::Completed),
            ("canceled", PromptStatus::Canceled),
        ] {
            let json =
                format!(r#"{{"type":"prompt_completed","request_id":"p1","status":"{status}"}}"#);
            let RpcFrame::PromptCompleted(completed) = decode_frame(json.as_bytes()).unwrap()
            else {
                panic!("wanted completion")
            };
            assert_eq!(completed.status, expected);
        }

        let RpcFrame::PromptCompleted(failed) = decode_frame(
            br#"{"type":"prompt_completed","request_id":"p1","status":"failed","error":"boom"}"#,
        )
        .unwrap() else {
            panic!("wanted completion")
        };
        assert_eq!(failed.status, PromptStatus::Failed);
        assert_eq!(failed.error.as_deref(), Some("boom"));
    }

    #[test]
    fn failed_completion_requires_error() {
        let error =
            decode_frame(br#"{"type":"prompt_completed","request_id":"p1","status":"failed"}"#)
                .unwrap_err();
        assert!(matches!(error, SnowError::Protocol(_)));
    }

    #[test]
    fn unknown_frame_is_retained() {
        let RpcFrame::Unknown(frame) =
            decode_frame(br#"{"type":"future_event","answer":42}"#).unwrap()
        else {
            panic!("wanted unknown")
        };
        assert_eq!(frame.kind, "future_event");
        assert_eq!(frame.fields["answer"], 42);
    }

    #[test]
    fn agent_event_preserves_additive_fields() {
        let RpcFrame::Agent(event) =
            decode_frame(br#"{"type":"text_delta","text":"hi","future":{"enabled":true}}"#)
                .unwrap()
        else {
            panic!("wanted agent event")
        };
        assert_eq!(event.string("text"), Some("hi"));
        assert!(event.fields.contains_key("future"));
    }

    #[test]
    fn malformed_interactions_retain_correlated_ids() {
        let RpcFrame::MalformedInteraction(user_input) =
            decode_frame(br#"{"type":"user_input_request","user_input":{"id":"ask-1"}}"#).unwrap()
        else {
            panic!("wanted malformed user input event")
        };
        assert_eq!(user_input.request_id.as_deref(), Some("ask-1"));

        let RpcFrame::MalformedInteraction(permission) = decode_frame(
            br#"{"type":"permission_request","permission":{"request":{"id":"perm-1"}}}"#,
        )
        .unwrap() else {
            panic!("wanted malformed permission event")
        };
        assert_eq!(permission.request_id.as_deref(), Some("perm-1"));
    }

    #[test]
    fn rejects_non_object_and_invalid_utf8() {
        assert!(matches!(
            decode_frame(br#"["not","an","object"]"#),
            Err(SnowError::Protocol(_))
        ));
        assert!(matches!(decode_frame(&[0xff]), Err(SnowError::InvalidUtf8)));
    }
}
