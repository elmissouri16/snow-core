#!/usr/bin/env python3
"""Smoke Snow's real TUI process in a controlled PTY (no accounts or network).

This emulates terminal protocol replies; it does not replace a live Ghostty
visual check. Requires a POSIX PTY and a built Snow binary.
"""

import argparse
import base64
import fcntl
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time


class Terminal:
    def __init__(self, binary, root, capabilities=True, mode=2, remote=False, descriptors=None):
        self.owns_pty = descriptors is None
        self.master, self.slave = descriptors or pty.openpty()
        self.original = termios.tcgetattr(self.slave)
        self.resize(100, 30)
        self.output = bytearray()
        self.pending = b""
        self.capabilities = capabilities
        self.mode = mode
        self.background = b"1010/1010/1010"
        self.clipboard = b"terminal clipboard fixture"
        self.queries = []
        env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"),
               "SNOW_HOME": str(root / "home"), "TERM": "xterm-ghostty",
               "TERM_PROGRAM": "ghostty", "COLORTERM": "truecolor"}
        if remote:
            env["SSH_TTY"] = "/dev/fixture"
        self.process = subprocess.Popen(
            [str(binary), "--provider", "fake", "--no-session", "--no-mcp",
             "--no-skills", "--no-plugins", "--no-subagents", "--no-debug",
             "--permission", "deny"], cwd=root, env=env,
            stdin=self.slave, stdout=self.slave, stderr=self.slave,
            start_new_session=True)

    def resize(self, width, height):
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ,
                    struct.pack("HHHH", height, width, height * 18, width * 9))
        if hasattr(self, "process"):
            self.process.send_signal(signal.SIGWINCH)

    def send(self, value):
        os.write(self.master, value)

    def pump(self, duration=0.15):
        deadline = time.monotonic() + duration
        while time.monotonic() < deadline:
            ready, _, _ = select.select([self.master], [], [], min(0.05, max(0, deadline-time.monotonic())))
            if not ready:
                continue
            data = os.read(self.master, 65536)
            self.output.extend(data)
            self.pending += data
            if self.capabilities:
                self.answer_queries()
            # Preserve partial escape queries split across renderer writes.
            self.pending = self.pending[-256:]

    def answer_queries(self):
        pattern = rb"\x1b\[\?(2026|2027|2031)\$p|\x1b\[\?u|\x1b\[c|\x1b\[6n|\x1b\]11;\?(?:\x07|\x1b\\)|\x1b\]52;c;\?(?:\x07|\x1b\\)"
        while match := re.search(pattern, self.pending):
            query = match.group()
            self.queries.append(query)
            if match.group(1):
                mode = match.group(1)
                setting = self.mode if mode == b"2031" else 2
                answer = b"\x1b[?" + mode + b";" + str(setting).encode() + b"$y"
            elif query == b"\x1b[?u":
                answer = b"\x1b[?1u"
            elif query == b"\x1b[c":
                answer = b"\x1b[?62;22c"
            elif query == b"\x1b[6n":
                answer = b"\x1b[1;1R"
            elif b"]52;" in query:
                answer = b"\x1b]52;c;" + base64.b64encode(self.clipboard) + b"\x07"
            else:
                answer = b"\x1b]11;rgb:" + self.background + b"\x07"
            self.pending = self.pending[match.end():]
            self.send(answer)

    def until(self, predicate, label, timeout=15):
        deadline = time.monotonic() + timeout
        while not predicate():
            if time.monotonic() > deadline:
                raise AssertionError("timed out: " + label)
            if self.process.poll() is not None:
                raise AssertionError("process exited while waiting for " + label)
            self.pump()

    def plain_output(self):
        return re.sub(rb"\x1b\[[0-?]*[ -/]*[@-~]", b"", self.output)

    def start(self):
        self.until(lambda: b"untrusted" in self.output.lower() or b"Type a message" in self.plain_output(),
                   "startup/trust prompt")
        if b"untrusted" in self.output.lower():
            self.send(b"\r")  # Explicitly continue without project trust.
        self.until(lambda: b"Type a message" in self.plain_output(), "composer")
        self.pump()

    def finish(self, cancel=False):
        if cancel:
            self.process.send_signal(signal.SIGTERM)
        else:
            self.send(b"\x03")
        deadline = time.monotonic() + 10
        while self.process.poll() is None and time.monotonic() < deadline:
            self.pump()
        self.pump()
        if self.process.poll() is None:
            raise AssertionError("TUI did not exit")
        if not cancel and self.process.returncode != 0:
            raise AssertionError(f"quit exit code {self.process.returncode}")
        restored = termios.tcgetattr(self.slave)
        expected = self.original.copy()
        # Darwin may mark queued input for redisplay when canonical mode
        # resumes. PENDIN is transient kernel state, not a terminal setting.
        pending_input = getattr(termios, "PENDIN", 0)
        restored[3] &= ~pending_input
        expected[3] &= ~pending_input
        if restored != expected:
            raise AssertionError(f"TTY settings were not restored: {expected!r} -> {restored!r}")
        if b"\x1b[?1049l" not in self.output:
            raise AssertionError("alternate screen was not restored")
        for reset in (b"\x1b[?25h", b"\x1b[?2004l", b"\x1b[?1004l"):
            if reset not in self.output:
                raise AssertionError(f"terminal mode not restored: {reset!r}")
        if self.capabilities:
            for sequence in (b"\x1b[?2026h", b"\x1b[?2026l", b"\x1b[?2027h", b"\x1b[?2027l", b"\x1b[<1u"):
                if sequence not in self.output:
                    raise AssertionError(f"enhanced mode negotiation/restoration missing: {sequence!r}")
        if self.capabilities and self.mode == 2:
            if b"\x1b[?2031h" not in self.output or b"\x1b[?2031l" not in self.output:
                raise AssertionError("owned appearance mode was not enabled/restored")
        elif b"\x1b[?2031l" in self.output:
            raise AssertionError("unowned appearance mode was reset")

    def close(self):
        if self.process.poll() is None:
            self.process.kill()
            self.process.wait(timeout=5)
        if self.owns_pty:
            os.close(self.master)
            os.close(self.slave)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    args = parser.parse_args()
    binary = args.binary.resolve()
    cases = [("enhanced-quit", True, 2, False),
             ("enhanced-cancel", True, 2, True),
             ("preenabled-mode", True, 1, False),
             ("legacy-no-replies", False, 0, False)]
    for name, capabilities, mode, cancel in cases:
        with tempfile.TemporaryDirectory(prefix="snow-terminal-") as directory:
            terminal = Terminal(binary, Path(directory), capabilities, mode, remote=capabilities)
            try:
                terminal.start()
                if capabilities:
                    terminal.send(b"first\x1b[13;2u")
                    terminal.send("日本語 é".encode())
                    terminal.send(b"\x1b[200~\n/help literal paste\x1b[201~")
                    terminal.until(lambda: "日本語".encode() in terminal.plain_output()
                                   and b"/help literal paste" in terminal.plain_output(),
                                   "Unicode and literal bracketed paste in composer")
                    terminal.send(b"\x15")  # Clear the current editor line.
                    terminal.send(b"\x16")  # Remote text clipboard read.
                    terminal.until(lambda: any(b"]52;" in q for q in terminal.queries), "OSC52 request")
                    terminal.until(lambda: terminal.clipboard in terminal.plain_output(), "OSC52 text in composer")
                    before = sum(b"]11;" in q for q in terminal.queries)
                    terminal.background = b"ffff/ffff/ffff"
                    terminal.send(b"\x1b[I")  # Focus triggers actual background query.
                    terminal.until(lambda: sum(b"]11;" in q for q in terminal.queries) > before, "focus background query")
                    terminal.resize(40, 12)
                    terminal.pump()
                    terminal.resize(100, 30)
                    terminal.send(b"\x1b[17~")  # F6 to native selection, then back.
                    terminal.pump()
                    terminal.send(b"\x1b[17~")
                    terminal.pump()
                terminal.finish(cancel)
                print(f"PASS {name}: input, resize, mode ownership, terminal restoration")
            except Exception:
                log = Path(tempfile.gettempdir()) / f"snow-terminal-{name}-failure.log"
                log.write_bytes(terminal.output)
                print(f"PTY output saved to {log}")
                raise
            finally:
                terminal.close()
    descriptors = pty.openpty()
    try:
        with tempfile.TemporaryDirectory(prefix="snow-handoff-") as directory:
            for capabilities in (True, False):
                terminal = Terminal(binary, Path(directory), capabilities, descriptors=descriptors)
                try:
                    terminal.start()
                    terminal.finish()
                finally:
                    terminal.close()
        print("PASS same-PTY restart handoff simulation: second process starts with restored terminal state")
    finally:
        for descriptor in descriptors:
            os.close(descriptor)


if __name__ == "__main__":
    main()
