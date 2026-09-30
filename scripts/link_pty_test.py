"""Link navigation through the real CLI, using only standard-library fixtures."""
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

ROOT = Path(__file__).resolve().parents[1]


def click(x, y):
    return f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode()


def run_link_case(name, path, steps, pixel=False, watch=False, opener_dir=None):
    """Each step waits for a new frame or a status message before sending input."""
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(ROOT)
        os.environ["PATH"] = str(opener_dir) if opener_dir else "/ma-test-no-executables"
        args = ["./bin/ma"] + (["--watch"] if watch else []) + [str(path)]
        os.execv(args[0], args)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 1000, 720))
    pending = bytearray()
    transcript = bytearray()
    frames = []
    peer = KittyPeer()
    mode_replied = False
    step = 0
    frame_start = 0
    message_start = 0
    exit_status = None
    tty_closed = False
    raw_restored = False
    quitting = False
    deadline = time.monotonic() + 30
    try:
        while time.monotonic() < deadline:
            ready, _, _ = select.select([fd], [], [], .05)
            if ready:
                try:
                    chunk = os.read(fd, 65536)
                except OSError as error:
                    if error.errno != errno.EIO:
                        raise
                    tty_closed = True
                    break
                if not chunk:
                    tty_closed = True
                    break
                transcript.extend(chunk)
                pending.extend(chunk)
                if not mode_replied and b"\x1b[?1016$p" in transcript:
                    mode_replied = True
                    os.write(fd, b"\x1b[?1016;2$y" if pixel else b"\x1b[?1016;0$y")
                while True:
                    found = re.search(rb"\x1b_G([^;]*);(.*?)\x1b\\", pending, re.S)
                    if not found:
                        break
                    header, data = found.groups()
                    del pending[:found.end()]
                    event, frame = peer.command(header, data)
                    if event == "query":
                        os.write(fd, b"\x1b_Gi=1073741820;OK\x1b\\\x1b[6;24;10t")
                    elif event == "frame":
                        frames.append(frame)
                if step < len(steps):
                    kind, expected, action = steps[step]
                    recent = transcript[message_start:]
                    ready = expected in recent and (kind != "frame" or len(frames) > frame_start)
                    if pixel and step == 0:
                        ready = ready and b"\x1b[?1016h" in transcript
                    if ready:
                        if callable(action):
                            action = action(frames, peer)
                        frame_start, message_start = len(frames), len(transcript)
                        step += 1
                        if action is not None:
                            os.write(fd, action)
                        if step == len(steps):
                            quitting = True
            child, status = os.waitpid(pid, os.WNOHANG)
            if child:
                exit_status = status
                break
        if exit_status is None:
            # On macOS a child can remain in tty teardown until the master is
            # closed. Check restored modes first, then release the master.
            if tty_closed:
                raw_restored = bool(termios.tcgetattr(fd)[3] & termios.ICANON)
                os.close(fd)
                fd = -1
            child, status = os.waitpid(pid, 0 if tty_closed else os.WNOHANG)
            if child:
                exit_status = status
        assert step == len(steps), f"{name}: stopped at step {step}: {transcript[-1800:]!r}"
        assert quitting and exit_status is not None, f"{name}: pager did not quit"
        assert os.waitstatus_to_exitcode(exit_status) == 0, transcript[-1800:]
        assert b"\x1b[?1016l" in transcript and b"\x1b[?1049l" in transcript
        assert raw_restored or termios.tcgetattr(fd)[3] & termios.ICANON, "raw mode was not restored"
        if pixel:
            assert b"\x1b[?1016h" in transcript, "pixel mouse mode was not enabled"
        (ROOT / "artifacts").mkdir(exist_ok=True)
        (ROOT / "artifacts" / f"pty-{name}.png").write_bytes(png(*frames[-1]))
        print(f"PASS {name}: {len(frames)} frames, link navigation and cleanup verified")
    finally:
        if exit_status is None:
            try:
                os.kill(pid, signal.SIGTERM)
                if fd >= 0:
                    os.close(fd)
                    fd = -1
                os.waitpid(pid, 0)
            except ProcessLookupError:
                pass
        if fd >= 0:
            os.close(fd)


def run_link_cases():
    with tempfile.TemporaryDirectory(prefix="ma-links-") as directory:
        root = Path(directory)
        (root / "sub").mkdir()
        first, second = root / "first.md", root / "sub" / "next.md"
        first.write_text("[Open next](sub/next.md#destination)\n\n" + "Reading the original document.\n\n" * 30)
        second.write_text("# Introduction\n\n" + "Before the destination.\n\n" * 20 + "# Destination\n\n[Go back](../first.md)\n\n" + "Destination content.\n\n" * 30)

        def verify_return(frames, peer):
            assert frames[0] == frames[-1], "back did not restore the original reading position"
            return b"l"

        for pixel in [False, True]:
            run_link_case("link-pixel" if pixel else "link-cell", first, [
                ("frame", b"first.md", click(51, 51) if pixel else click(5, 2)),
                ("frame", b"next.md", b"h"),
                ("frame", b"first.md", verify_return),
                ("frame", b"next.md", b"q"),
            ], pixel=pixel)

        run_link_case("link-keyboard", first, [
            ("frame", b"first.md", b"\t"),
            ("frame", b"sub/next.md#destination", b"\r"),
            ("frame", b"next.md", b"+"),
            ("frame", b"next.md", b"\x1b[1;3D"),
            ("frame", b"first.md", b"\x1b[1;3C"),
            ("frame", b"next.md", b"\x1b[Z"),
            ("frame", b"../first.md", b"\r"),
            ("frame", b"first.md", b"q"),
        ])

        def edit_destination(frames, peer):
            assert peer.uploads == 2, "unexpected uploads before watch edit"
            second.write_text("# Destination\n\nAutomatically updated destination.\n")
            return None

        run_link_case("link-watch", first, [
            ("frame", b"first.md", click(5, 2)),
            ("frame", b"next.md", edit_destination),
            ("frame", b"next.md", b"h"),
            ("frame", b"first.md", b"q"),
        ], watch=True)

        def edit_for_reload(frames, peer):
            second.write_text("# Destination\n\nManually reloaded destination.\n")
            return b"r"

        run_link_case("link-reload", first, [
            ("frame", b"first.md", click(5, 2)),
            ("frame", b"next.md", edit_for_reload),
            ("frame", b"next.md", b"q"),
        ])

        broken = root / "broken.md"
        broken.write_text("[Missing](missing.md) [Unknown](sub/next.md#absent) [Unsafe](javascript:alert)\n")
        run_link_case("link-errors", broken, [
            ("frame", b"broken.md", b"\t\r"),
            ("status", b"no such file", b"\t\r"),
            ("status", b"Heading not found", b"\t\r"),
            ("status", b"unsupported link scheme", b"q"),
        ])

        anchors = root / "anchors.md"
        anchors.write_text("[Jump](#destination)\n\n" + "Earlier text.\n\n" * 30 + "# Destination\n\n" + "Later text.\n\n" * 30)
        def verify_anchor_return(frames, peer):
            assert frames[0] == frames[-1], "anchor back lost reading position"
            return b"q"

        run_link_case("link-anchor", anchors, [
            ("frame", b"anchors.md", click(5, 2)),
            ("frame", b"anchors.md", b"h"),
            ("frame", b"anchors.md", verify_anchor_return),
        ])

        web = root / "web.md"
        url = "https://example.com/?q=$(ignored)&x=literal"
        web.write_text(f"[Website]({url})\n")
        opener_dir = root / "opener"
        opener_dir.mkdir()
        log = opener_dir / "arguments"
        for program in ["open", "xdg-open"]:
            stub = opener_dir / program
            stub.write_text(f"#!/bin/sh\nprintf '%s\\n' \"$#\" \"$1\" >> '{log}'\n")
            stub.chmod(0o700)
        run_link_case("link-browser", web, [
            ("frame", b"web.md", click(5, 2)),
            ("status", b"Opened https://example.com/", b"q"),
        ], opener_dir=opener_dir)
        assert log.read_text().splitlines() == ["1", url], "browser opened twice or URL was interpreted by a shell"
        run_link_case("link-browser-missing", web, [
            ("frame", b"web.md", click(5, 2)),
            ("status", b"cannot open browser", b"q"),
        ])


if __name__ == "__main__":
    run_link_cases()
