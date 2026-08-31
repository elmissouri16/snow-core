mod client;
mod error;
mod process;
mod protocol;

pub(crate) use client::ShutdownTracker;
pub use client::{RuntimeEvent, SnowClient, SnowConnection, completion_summary};
pub use error::SnowError;
pub use process::RuntimeConfig;
pub use protocol::{
    BranchCatalog, HistoryMessage, ModelCatalog, ModelInfo, PromptCompleted, PromptStatus,
    RpcReady, SessionBranch, SessionInfo,
};
