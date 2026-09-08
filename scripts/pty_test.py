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
import time
from kitty_peer import KittyPeer, png

ROOT = Path(__file__).resolve().parents[1]


def run_case(keys, name, piped=False, unsupported=False, cached=False):
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
        args = ["./bin/ma"] + ([] if piped else ["examples/showcase.md"])
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
                        if not unsupported:
                            os.write(fd, b"\x1b_Gi=1073741820;OK\x1b\\\x1b[6;24;10t")
                    elif event == "frame":
                        frames.append(frame)
                        if cached and 1 < len(frames) < 12:
                            os.write(fd, b"j")
                if frames and not sent:
                    sent = True
                    os.write(fd, keys)
                    # Resize while interacting, then leave time for the final
                    # frame to arrive before testing clean shutdown.
                    if not cached:
                        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 28, 80, 800, 672))
                        os.kill(pid, signal.SIGWINCH)
                if len(frames) >= (12 if cached else 2) and sent and not quitting:
                    quitting = True
                    if name == "signal":
                        os.kill(pid, signal.SIGTERM)
                    else:
                        os.write(fd, b"q")
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
            if cached:
                assert peer.uploads == 1, f"cached scrolling uploaded pixels {peer.uploads} times"
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
