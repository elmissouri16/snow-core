mod client;
mod error;
mod process;
mod protocol;

pub(crate) use client::ShutdownTracker;
pub use client::{
    RuntimeEvent, SnowClient, SnowConnection, SubagentMessagesRequest, completion_summary,
};
pub use error::SnowError;
pub use process::RuntimeConfig;
pub use protocol::{
    AdaptiveColor, AgentRef, AuthLoginJob, AuthMethod, AuthProgress, AuthProvider,
    AuthProviderList, AuthStatus, BranchCatalog, GoalSummary, HistoryBlock, HistoryEntry,
    HistoryImage, HistoryToolCall, HistoryToolDisplay, HistoryToolResult, InteractionKind,
    KeybindingAction, KeybindingScope, Keybindings, KeybindingsUpdateParams,
    MAX_HISTORY_IMAGE_BYTES, MAX_HISTORY_IMAGES, MAX_HISTORY_TOOL_PROGRESS_ITEMS,
    MAX_HISTORY_TOOL_TEXT_CHARS, MAX_KEYBINDING_TEXT_CHARS, MAX_PRESENTATION_DATA_BYTES,
    MAX_THEME_CATALOG_ITEMS, MAX_THEME_TEXT_CHARS, ManagedProcess, ManagedProcessList,
    ManagedProcessLogs, ModelCatalog, ModelInfo, ModelPricing, ModelUpgrade, PendingInputCounts,
    PermissionDecision, PermissionRequest, PromptCompleted, PromptStatus, RpcReady, SessionBranch,
    SessionInfo, SessionList, SessionSummary, Settings, SubagentHistoryMessage, SubagentLimits,
    SubagentList, SubagentMessagesPage, SubagentMessagesParams, SubagentState, ThemeCatalog,
    ThemeColors, ThemeDescriptor, ThemeSettingsUpdateParams, UserInputAnswer, UserInputOption,
    UserInputQuestion, UserInputRequest, decode_history_entries, decode_keybindings,
    decode_settings, decode_subagent_messages_page, decode_theme_catalog,
    validate_keybindings_update, validate_theme_selection,
};
