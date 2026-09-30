#!/usr/bin/env python3
"""Exercise ma as a real terminal process with a minimal Kitty peer.

Requires a built bin/ma. No browser or third-party Python packages.
"""
import errno
import fcntl
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import termios
import tempfile
import time
from kitty_peer import KittyPeer, png
from link_pty_test import run_link_cases

ROOT = Path(__file__).resolve().parents[1]


def run_case(keys, name, piped=False, unsupported=False, cached=False,
             path="examples/showcase.md", resize=True, actions=None, watch=False,
             force=False, size_reply=True):
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(ROOT)
        # The native pager must work without any executable on PATH.
        os.environ["PATH"] = "/ma-test-no-executables"
        os.environ["MA_BROWSER"] = "/ma-test-no-browser"
        if piped:
            source = os.open("examples/showcase.md", os.O_RDONLY)
            os.dup2(source, 0)
            os.close(source)
        args = (["./bin/ma"] + (["--watch"] if watch else [])
                + (["--force-graphics"] if force else []) + ([] if piped else [str(path)]))
        os.execv(args[0], args)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 1000, 720))
    transcript = bytearray()
    frames = []
    pending = bytearray()
    peer = KittyPeer()
    queried = False
    sent = False
    quitting = False
    eof = False
    deadline = time.monotonic() + 25
    exit_status = None
    reload_at = None
    reloaded = False
    target_frames = 12 if cached else len(actions)+1 if actions else 2
    try:
        while time.monotonic() < deadline:
            ready, _, _ = select.select([fd], [], [], .05)
            if ready:
                try:
                    chunk = os.read(fd, 65536)
                except OSError as error:
                    if error.errno == errno.EIO:
                        eof = True
                        break
                    raise
                if not chunk:
                    eof = True
                    break
                transcript.extend(chunk)
                pending.extend(chunk)
                while True:
                    found = re.search(rb"\x1b_G([^;]*);(.*?)\x1b\\", pending, re.S)
                    if not found:
                        break
                    header, data = found.groups()
                    del pending[:found.end()]
                    event, frame = peer.command(header, data)
                    if event == "query":
                        queried = True
                        if not unsupported and not force:
                            os.write(fd, b"\x1b_Gi=1073741820;OK\x1b\\")
                            if size_reply:
                                os.write(fd, b"\x1b[6;24;10t")
                    elif event == "frame":
                        frames.append(frame)
                        if cached and 1 < len(frames) < 12:
                            os.write(fd, b"j")
                        if actions and 1 < len(frames) <= len(actions):
                            os.write(fd, actions[len(frames)-1])
                if frames and not sent:
                    sent = True
                    os.write(fd, actions[0] if actions else keys)
                    if watch:
                        reload_at = time.monotonic() + 1.1
                    # Resize while interacting, then leave time for the final
                    # frame to arrive before testing clean shutdown.
                    if not cached and resize:
                        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 28, 80, 800, 672))
                        os.kill(pid, signal.SIGWINCH)
                if len(frames) >= target_frames and sent and not quitting:
                    quitting = True
                    if name == "signal":
                        os.kill(pid, signal.SIGTERM)
                    else:
                        os.write(fd, b"q")
            if reload_at and time.monotonic() >= reload_at and not reloaded:
                assert peer.uploads == 1, "watch uploaded unchanged content while idle"
                replacement = Path(str(path) + ".new")
                replacement.write_text("# Reloaded automatically\n\nUpdated content after an atomic editor save.\n")
                replacement.replace(path)
                reloaded = True
            child, status = os.waitpid(pid, 0 if eof else os.WNOHANG)
            if child:
                exit_status = status
                break
        if exit_status is None:
            child, status = os.waitpid(pid, 0 if eof else os.WNOHANG)
            if child:
                exit_status = status
        assert exit_status is not None, "pager did not exit within timeout"
        assert queried, f"no Kitty capability query: {transcript[-2000:]!r}"
        if unsupported:
            assert os.waitstatus_to_exitcode(exit_status) == 1
            assert b"did not acknowledge" in transcript
            assert b"\x1b[?1049h" not in transcript
        else:
            assert os.waitstatus_to_exitcode(exit_status) == 0, transcript[-2000:]
            assert len(frames) >= 2, "no updated frame"
            assert frames[0] != frames[-1], "scroll/search/resize did not change the frame"
            assert b"\x1b[?1049h" in transcript and b"\x1b[?1049l" in transcript
            assert b"\x1b[?25h" in transcript and b"a=d,d=I" in transcript
            assert not (termios.tcgetattr(fd)[3] & termios.ICANON) == 0, "raw mode was not restored"
            if name == "search":
                assert b"match 1/" in transcript, "search did not find rendered text"
            if name == "search-reflow":
                assert b"match 1/" in transcript and b"match 2/" in transcript, "search selection was lost during zoom"
            if cached:
                assert peer.uploads == 1, f"cached scrolling uploaded pixels {peer.uploads} times"
            if watch:
                assert reloaded and peer.uploads == 2, "watch did not reload the changed file exactly once"
                assert b" | watch | " in transcript, "missing watch indicator"
            if name == "headings":
                assert b"A familiar reading experience" in transcript and b"Details that matter" in transcript
                assert frames[1] == frames[3], "previous heading did not return to the same pixels"
            (ROOT / "artifacts").mkdir(exist_ok=True)
            (ROOT / "artifacts" / f"pty-{name}.png").write_bytes(png(*frames[-1]))
        print(f"PASS {name}: {len(frames)} frames, {peer.uploads} pixel uploads, detection and cleanup verified")
    finally:
        if exit_status is None:
            try:
                os.kill(pid, signal.SIGTERM)
                os.waitpid(pid, 0)
            except ProcessLookupError:
                pass
        os.close(fd)


if __name__ == "__main__":
    run_case(b"G", "scroll")
    run_case(b"/typography\r", "search", piped=True)
    run_case(b"j", "signal")
    run_case(b"", "unsupported", unsupported=True)
    run_case(b"j", "cached-scroll", cached=True)
    run_case(b"", "headings", resize=False, actions=[b"]", b"]", b"["])
    run_case(b"", "zoom-theme", resize=False, actions=[b"d", b"+", b"-", b"t"])
    run_case(b"", "search-reflow", resize=False, actions=[b"/typography\r", b"+", b"n"])
    run_case(b"j", "force-graphics", resize=False, force=True)
    run_case(b"j", "ioctl-pixels", resize=False, size_reply=False)
    with tempfile.TemporaryDirectory(prefix="ma-watch-") as directory:
        path = Path(directory) / "notes.md"
        path.write_text("# Before editing\n\nAn unchanged file should stay cached.\n")
        run_case(b"", "watch", path=path, resize=False, watch=True)
    run_link_cases()
