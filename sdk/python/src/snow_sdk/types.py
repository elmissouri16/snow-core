"""Small typed wrappers that preserve forward-compatible raw RPC objects."""

from dataclasses import dataclass
from typing import Any, Dict, Tuple


JSONDict = Dict[str, Any]


@dataclass(frozen=True)
class RPCReady:
    protocol_version: str
    snow_version: str
    capabilities: Tuple[str, ...]
    max_input_bytes: int
    raw: JSONDict


@dataclass(frozen=True)
class AgentEvent:
    type: str
    raw: JSONDict

    def get(self, key: str, default: Any = None) -> Any:
        return self.raw.get(key, default)


@dataclass(frozen=True)
class PromptResult:
    request_id: str
    status: str
    raw: JSONDict
