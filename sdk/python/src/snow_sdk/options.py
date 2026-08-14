"""Process options for the local Snow RPC client."""

import math
from dataclasses import dataclass, field
from typing import Mapping, Optional, Tuple


@dataclass(frozen=True)
class SnowOptions:
    """Configure the external ``snow --mode rpc`` process.

    The defaults are intentionally headless and restrictive. Broader authority
    must be enabled explicitly by the embedding application.
    """

    command: Tuple[str, ...] = ("snow",)
    cwd: Optional[str] = None
    provider: str = "fake"
    model: str = ""
    base_url: str = ""
    session_path: str = ""
    permission: str = "deny"
    thinking: str = "off"
    disable_plugins: bool = True
    disable_mcp: bool = True
    disable_skills: bool = True
    disable_subagents: bool = True
    environment: Optional[Mapping[str, str]] = field(default=None, repr=False)
    inherit_environment: bool = True
    startup_timeout: float = 10.0
    close_timeout: float = 5.0
    request_timeout: float = 120.0
    max_frame_bytes: int = 16 * 1024 * 1024
    event_queue_size: int = 256

    def argv(self) -> Tuple[str, ...]:
        if not self.command:
            raise ValueError("command must not be empty")
        args = list(self.command)
        args.extend(("--mode", "rpc", "--provider", self.provider))
        args.extend(("--permission", self.permission, "--thinking", self.thinking))
        if self.model:
            args.extend(("--model", self.model))
        if self.base_url:
            args.extend(("--base-url", self.base_url))
        if self.session_path:
            args.extend(("--session", self.session_path))
        else:
            args.append("--no-session")
        if self.disable_plugins:
            args.append("--no-plugins")
        if self.disable_mcp:
            args.append("--no-mcp")
        if self.disable_skills:
            args.append("--no-skills")
        if self.disable_subagents:
            args.append("--no-subagents")
        return tuple(args)

    def validate(self) -> None:
        if not self.command or any(not isinstance(part, str) or not part for part in self.command):
            raise ValueError("command must contain non-empty strings")
        if any(not math.isfinite(value) or value <= 0 for value in (self.startup_timeout, self.close_timeout, self.request_timeout)):
            raise ValueError("timeouts must be finite and positive")
        if not isinstance(self.max_frame_bytes, int) or not isinstance(self.event_queue_size, int) or self.max_frame_bytes <= 0 or self.event_queue_size <= 0:
            raise ValueError("frame and queue limits must be positive integers")
