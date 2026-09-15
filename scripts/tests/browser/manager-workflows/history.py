"""Read bounded durable state only from this runner's private fixture sessions."""
import json
import os
from pathlib import Path
import sqlite3
import sys
from urllib.parse import quote

root = Path(sys.argv[1]).resolve(strict=True)
if not root.is_dir() or root.name != "sessions":
    raise SystemExit("Expected private fixture sessions directory")
result = []
paths = []
for directory, dirs, files in os.walk(root, followlinks=False):
    dirs[:] = [name for name in dirs if not (Path(directory) / name).is_symlink()]
    for name in files:
        path = Path(directory) / name
        if path.suffix != ".db" or path.is_symlink():
            continue
        if not path.resolve(strict=True).is_relative_to(root):
            raise SystemExit("Session path escaped private fixture")
        if path.stat().st_size > 16 * 1024 * 1024:
            raise SystemExit("Fixture session exceeds 16 MiB")
        paths.append(path)
        if len(paths) > 16:
            raise SystemExit("Too many fixture session files")
for path in paths:
    with sqlite3.connect("file:" + quote(str(path)) + "?mode=ro", uri=True, timeout=2) as db:
        db.execute("PRAGMA query_only=ON")
        session_id, branch_tip = db.execute("SELECT session_id,branch_tip FROM session_meta WHERE singleton=1").fetchone()
        entries = []
        for entry_id, parent, kind, raw, key, value in db.execute("SELECT id,parent_id,entry_type,message,meta_key,meta_value FROM entries ORDER BY seq LIMIT 1001"):
            if len(entries) >= 1000:
                raise SystemExit("Fixture history exceeds 1000 entries")
            message = json.loads(raw) if raw else {}
            # Never export opaque/private blocks; fixture checks only user text,
            # ordinary assistant text, durable IDs and root-run markers.
            entries.append({"id": entry_id, "parent": parent, "kind": kind,
                            "role": message.get("role", ""),
                            "text": "".join(b.get("text", "") for b in message.get("content", []) if b.get("type") == "text"),
                            "marker": value if key == "agent_turn_v1" else ""})
        result.append({"session_id": session_id, "branch_tip": branch_tip, "entries": entries})
output = json.dumps(result)
if len(output.encode("utf-8")) > 1024 * 1024:
    raise SystemExit("Fixture history summary exceeds 1 MiB")
print(output)
