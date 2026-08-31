mod client;
mod error;
mod process;
mod protocol;

pub(crate) use client::ShutdownTracker;
pub use client::{RuntimeEvent, SnowClient, SnowConnection, completion_summary};
pub use error::SnowError;
pub use process::RuntimeConfig;
pub use protocol::{
    BranchCatalog, HistoryMessage, InteractionKind, ModelCatalog, ModelInfo, PermissionDecision,
    PermissionRequest, PromptCompleted, PromptStatus, RpcReady, SessionBranch, SessionInfo,
    UserInputAnswer, UserInputOption, UserInputQuestion, UserInputRequest,
};
