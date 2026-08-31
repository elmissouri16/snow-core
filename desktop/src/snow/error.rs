use std::{io, path::PathBuf};

#[derive(Debug, thiserror::Error)]
pub enum SnowError {
    #[error("Snow executable was not found: {0}")]
    ExecutableNotFound(PathBuf),
    #[error("failed to start Snow: {0}")]
    Spawn(#[source] io::Error),
    #[error("Snow did not become ready before the startup timeout")]
    StartupTimeout,
    #[error("the first Snow output frame was not rpc_ready (received {0})")]
    InvalidFirstFrame(String),
    #[error("unsupported Snow RPC protocol version {0}; expected 1")]
    UnsupportedProtocol(String),
    #[error("Snow RPC is missing required capability {0}")]
    MissingCapability(String),
    #[error("Snow RPC frame exceeded the {limit}-byte limit")]
    FrameTooLarge { limit: usize },
    #[error("Snow RPC emitted invalid UTF-8")]
    InvalidUtf8,
    #[error("Snow RPC emitted invalid JSON: {0}")]
    InvalidJson(String),
    #[error("Snow RPC protocol error: {0}")]
    Protocol(String),
    #[error("Snow rejected the request: {0}")]
    RequestRejected(String),
    #[error("the active prompt failed: {0}")]
    PromptFailed(String),
    #[error("Snow RPC is not ready")]
    NotReady,
    #[error("a root prompt is already running")]
    PromptAlreadyRunning,
    #[error("there is no active prompt to stop")]
    NoActivePrompt,
    #[error("a stop request is already pending")]
    AbortAlreadyRequested,
    #[error("the Snow RPC command queue is full")]
    CommandQueueFull,
    #[error("the Snow RPC command channel is closed")]
    ChannelClosed,
    #[error("Snow exited unexpectedly (status: {0:?})")]
    ProcessExited(Option<i32>),
    #[error("Snow did not stop before the shutdown timeout")]
    ShutdownTimeout,
    #[error("Snow RPC I/O failed: {0}")]
    Io(#[source] io::Error),
}

impl SnowError {
    pub(crate) fn io(error: io::Error) -> Self {
        Self::Io(error)
    }
}
