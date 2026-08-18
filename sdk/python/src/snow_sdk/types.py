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


@dataclass(frozen=True)
class QueuedInput:
    id: str
    kind: str
    text: str
    order: int
    raw: JSONDict


@dataclass(frozen=True)
class PendingInputs:
    items: Tuple[QueuedInput, ...]
    raw: JSONDict

    @classmethod
    def from_response(cls, response: JSONDict) -> "PendingInputs":
        raw = response.get("data", {})
        items = tuple(
            QueuedInput(
                id=item.get("id", ""),
                kind=item.get("kind", ""),
                text=item.get("text", ""),
                order=item.get("order", 0),
                raw=item,
            )
            for item in raw.get("items", [])
            if isinstance(item, dict)
        )
        return cls(items=items, raw=raw)


@dataclass(frozen=True)
class UsageSnapshot:
    input_tokens: int
    output_tokens: int
    reasoning_tokens: int
    cache_read: int
    cache_write: int
    total_tokens: int
    requests: int
    raw: JSONDict

    @classmethod
    def from_response(cls, response: JSONDict) -> "UsageSnapshot":
        data = response.get("data", {})
        return cls(
            input_tokens=data.get("input", 0),
            output_tokens=data.get("output", 0),
            reasoning_tokens=data.get("reasoning", 0),
            cache_read=data.get("cache_read", 0),
            cache_write=data.get("cache_write", 0),
            total_tokens=data.get("total_tokens", 0),
            requests=data.get("requests", 0),
            raw=data,
        )


@dataclass(frozen=True)
class CompactionResult:
    summarized_messages: int
    retained_messages: int
    summary: str
    used_fallback: bool
    automatic: bool
    raw: JSONDict

    @classmethod
    def from_response(cls, response: JSONDict) -> "CompactionResult":
        data = response.get("data", {})
        return cls(
            summarized_messages=data.get("summarized_messages", 0),
            retained_messages=data.get("retained_messages", 0),
            summary=data.get("summary", ""),
            used_fallback=data.get("used_fallback", False),
            automatic=data.get("automatic", False),
            raw=data,
        )


@dataclass(frozen=True)
class SessionBranch:
    id: str
    name: str
    parent_branch_id: str
    forked_from_id: str
    tip_id: str
    messages: int
    preview: str
    created_at: int
    updated_at: int
    active: bool
    raw: JSONDict

    @classmethod
    def from_raw(cls, item: JSONDict) -> "SessionBranch":
        return cls(
            id=item.get("id", ""),
            name=item.get("name", ""),
            parent_branch_id=item.get("parent_branch_id", ""),
            forked_from_id=item.get("forked_from_id", ""),
            tip_id=item.get("tip_id", ""),
            messages=item.get("messages", 0),
            preview=item.get("preview", ""),
            created_at=item.get("created_at", 0),
            updated_at=item.get("updated_at", 0),
            active=item.get("active", False),
            raw=item,
        )


@dataclass(frozen=True)
class BranchesList:
    branches: Tuple[SessionBranch, ...]
    raw: JSONDict

    @classmethod
    def from_response(cls, response: JSONDict) -> "BranchesList":
        data = response.get("data", {})
        branches = tuple(
            SessionBranch.from_raw(item)
            for item in data.get("branches", [])
            if isinstance(item, dict)
        )
        return cls(branches=branches, raw=data)


@dataclass(frozen=True)
class ContentBlock:
    type: str
    raw: JSONDict

    @classmethod
    def from_raw(cls, item: JSONDict) -> "ContentBlock":
        return cls(type=item.get("type", ""), raw=item)


@dataclass(frozen=True)
class MessagesList:
    messages: Tuple[JSONDict, ...]
    raw: JSONDict

    @classmethod
    def from_response(cls, response: JSONDict) -> "MessagesList":
        data = response.get("data", {})
        items = tuple(item for item in data.get("messages", []) if isinstance(item, dict))
        return cls(messages=items, raw=data)


@dataclass(frozen=True)
class ConfigDiagnostic:
    path: str
    message: str
    raw: JSONDict

    @classmethod
    def from_raw(cls, item: JSONDict) -> "ConfigDiagnostic":
        return cls(path=item.get("path", ""), message=item.get("message", ""), raw=item)


@dataclass(frozen=True)
class DiagnosticsList:
    diagnostics: Tuple[ConfigDiagnostic, ...]
    raw: JSONDict

    @classmethod
    def from_response(cls, response: JSONDict) -> "DiagnosticsList":
        data = response.get("data", {})
        items = tuple(
            ConfigDiagnostic.from_raw(item)
            for item in data.get("diagnostics", [])
            if isinstance(item, dict)
        )
        return cls(diagnostics=items, raw=data)


@dataclass(frozen=True)
class MCPServer:
    id: str
    transport: str
    connected: bool
    protocol_version: str
    server_name: str
    server_version: str
    capabilities: tuple = ()
    tool_count: int = 0
    message: str = ""
    state: str = ""
    cached: bool = False
    cached_at: str = ""
    last_used_at: str = ""
    raw: JSONDict = None

    @classmethod
    def from_raw(cls, item: JSONDict) -> "MCPServer":
        return cls(
            id=item.get("id", ""),
            transport=item.get("transport", ""),
            connected=bool(item.get("connected", False)),
            protocol_version=item.get("protocol_version", ""),
            server_name=item.get("server_name", ""),
            server_version=item.get("server_version", ""),
            capabilities=tuple(item.get("capabilities", [])),
            tool_count=item.get("tool_count", 0),
            message=item.get("message", ""),
            state=item.get("state", ""),
            cached=bool(item.get("cached", False)),
            cached_at=item.get("cached_at", ""),
            last_used_at=item.get("last_used_at", ""),
            raw=item,
        )


@dataclass(frozen=True)
class MCPServers:
    servers: Tuple[MCPServer, ...]
    raw: JSONDict

    @classmethod
    def from_response(cls, response: JSONDict) -> "MCPServers":
        data = response.get("data", {})
        items = tuple(
            MCPServer.from_raw(item) for item in data.get("servers", []) if isinstance(item, dict)
        )
        return cls(servers=items, raw=data)


@dataclass(frozen=True)
class Skill:
    name: str
    location: str
    scope: str
    source: str
    enabled: bool
    description: str = ""
    license: str = ""
    compatibility: str = ""
    metadata: dict = None
    allowed_tools: str = ""
    disabled_by: str = ""
    raw: JSONDict = None

    @classmethod
    def from_raw(cls, item: JSONDict) -> "Skill":
        return cls(
            name=item.get("name", ""),
            location=item.get("location", ""),
            scope=item.get("scope", ""),
            source=item.get("source", ""),
            enabled=bool(item.get("enabled", False)),
            description=item.get("description", ""),
            license=item.get("license", ""),
            compatibility=item.get("compatibility", ""),
            metadata=dict(item.get("metadata") or {}),
            allowed_tools=item.get("allowed_tools", ""),
            disabled_by=item.get("disabled_by", ""),
            raw=item,
        )


@dataclass(frozen=True)
class SkillDiagnostic:
    path: str
    skill: str
    level: str
    message: str
    raw: JSONDict

    @classmethod
    def from_raw(cls, item: JSONDict) -> "SkillDiagnostic":
        return cls(
            path=item.get("path", ""),
            skill=item.get("skill", ""),
            level=item.get("level", ""),
            message=item.get("message", ""),
            raw=item,
        )


@dataclass(frozen=True)
class Skills:
    skills: Tuple[Skill, ...]
    diagnostics: Tuple[SkillDiagnostic, ...]
    raw: JSONDict

    @classmethod
    def from_response(cls, response: JSONDict) -> "Skills":
        data = response.get("data", {})
        items = tuple(Skill.from_raw(item) for item in data.get("skills", []) if isinstance(item, dict))
        diags = tuple(
            SkillDiagnostic.from_raw(item) for item in data.get("diagnostics", []) if isinstance(item, dict)
        )
        return cls(skills=items, diagnostics=diags, raw=data)


@dataclass(frozen=True)
class SandboxStatus:
    configured: bool
    active: bool
    backend: str
    machine: str
    profile: str
    guest_cwd: str
    read_only: bool
    network: bool
    cpus: int
    memory_mib: int
    storage_gib: int
    overlay_gib: int
    raw: JSONDict

    @classmethod
    def from_response(cls, response: JSONDict) -> "SandboxStatus":
        data = response.get("data", {})
        status = data.get("status") or {}
        return cls(
            configured=bool(status.get("configured", False)),
            active=bool(status.get("active", False)),
            backend=status.get("backend", ""),
            machine=status.get("machine", ""),
            profile=status.get("profile", ""),
            guest_cwd=status.get("guest_cwd", ""),
            read_only=bool(status.get("read_only", False)),
            network=bool(status.get("network", False)),
            cpus=status.get("cpus", 0),
            memory_mib=status.get("memory_mib", 0),
            storage_gib=status.get("storage_gib", 0),
            overlay_gib=status.get("overlay_gib", 0),
            raw=data,
        )
