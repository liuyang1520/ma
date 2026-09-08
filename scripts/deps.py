#!/usr/bin/env python3
"""Age-gated Go proxy. Uses only Python's standard library.

Registry metadata is inspected before any .mod or .zip is handed to Go.
The full selected graph is checked even when Go has cached the modules.
"""
import datetime as dt
import email.utils
import http.server
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import threading
import urllib.error
import urllib.request
import urllib.parse

ROOT = Path(__file__).resolve().parents[1]
PROXY = "https://proxy.golang.org"
MIN_AGE = dt.timedelta(days=7)
NOW = dt.datetime.now(dt.timezone.utc)
CACHE = {}
LOCK = threading.Lock()
EVIDENCE_PATH = ROOT / "dependencies.json"
EVIDENCE = json.loads(EVIDENCE_PATH.read_text()) if EVIDENCE_PATH.exists() else {}


def fetch(url):
    req = urllib.request.Request(url, headers={"User-Agent": "ma-dependency-age-check/0.1"})
    with urllib.request.urlopen(req, timeout=30) as response:
        return response.read()


def timestamp(value):
    return dt.datetime.fromisoformat(value.replace("Z", "+00:00"))


def check_age(published, now=NOW):
    if not published or published.tzinfo is None or now - published < MIN_AGE:
        raise ValueError("release must be at least seven full days old")


def registry_hint(module, version, fallback):
    # The proxy's object creation time is a useful search hint for a tag
    # published long after its commit. It is never accepted as age evidence.
    req = urllib.request.Request(f"{PROXY}/{module}/@v/{version}.mod", method="HEAD")
    with urllib.request.urlopen(req, timeout=30) as response:
        value = response.headers.get("Last-Modified")
    return email.utils.parsedate_to_datetime(value) - dt.timedelta(minutes=5) if value else fallback


def indexed_at(module, version, revision_time):
    """Require public observation, not merely an old Git commit.

    The index is append-only. Cached timestamps are hints only: the exact
    record is fetched and verified again before accepting it.
    """
    path = re.sub(r"!([a-z])", lambda m: m[1].upper(), module)
    key = path + "@" + version
    hint = EVIDENCE.get(key)
    since = timestamp(hint) - dt.timedelta(microseconds=1) if hint else registry_hint(module, version, revision_time)
    for _ in range(100):
        params = urllib.parse.urlencode({"since": since.isoformat(), "limit": 2000})
        rows = [json.loads(line) for line in fetch("https://index.golang.org/index?" + params).splitlines()]
        for row in rows:
            if row["Path"] == path and row["Version"] == version:
                observed = timestamp(row["Timestamp"])
                check_age(observed)
                with LOCK:
                    EVIDENCE[key] = row["Timestamp"]
                return observed
        if not rows or timestamp(rows[-1]["Timestamp"]) > NOW - MIN_AGE:
            break
        since = timestamp(rows[-1]["Timestamp"])
    raise ValueError(f"no public registry observation at least seven days old for {key}")


def verify(module, version):
    key = (module, version)
    with LOCK:
        if key in CACHE:
            return CACHE[key]
    data = json.loads(fetch(f"{PROXY}/{module}/@v/{version}.info"))
    released = timestamp(data["Time"])
    check_age(released)
    published = None
    # GitHub publication time can be later than the tagged commit timestamp.
    # When a release exists, require both timestamps to have aged seven days.
    if module.startswith("github.com/") and not re.search(r"-\d{14}-[0-9a-f]+$", version):
        repo = "/".join(module.split("/")[1:3])
        try:
            release = json.loads(fetch(f"https://api.github.com/repos/{repo}/releases/tags/{version}"))
            published = timestamp(release["published_at"])
            check_age(published)
            released = max(released, published)
        except urllib.error.HTTPError as error:
            if error.code != 404:
                raise  # Unavailable metadata fails closed.
    # Release-less modules and pseudo-versions need registry observation
    # evidence. GitHub's published_at is already publication evidence.
    observed = published if published else indexed_at(module, version, released)
    with LOCK:
        CACHE[key] = max(released, observed).isoformat()
    return CACHE[key]


class Proxy(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_GET(self):
        match = re.fullmatch(r"/(.+)/@v/(v[^/]+)\.(info|mod|zip)", self.path)
        if not match:
            self.send_error(404, "only pinned module versions are allowed")
            return
        try:
            module, version, _ = match.groups()
            verify(module, version)
            body = fetch(PROXY + self.path)
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        except Exception as error:
            print(f"BLOCKED {self.path}: {error}", file=sys.stderr)
            self.send_error(403, "dependency age could not be verified")


def main():
    action = sys.argv[1] if len(sys.argv) == 2 else "check"
    if action not in ("check", "download", "tidy"):
        raise SystemExit("usage: python3 scripts/deps.py [check|download|tidy]")
    # replace/exclude directives could evade the version registry checks.
    if re.search(r"(?m)^\s*(replace|exclude)\b", (ROOT / "go.mod").read_text()):
        raise SystemExit("dependency policy forbids replace/exclude directives")
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Proxy)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    env = dict(os.environ, GOPROXY=f"http://127.0.0.1:{server.server_port}",
               GONOPROXY="none", GOPRIVATE="", GONOSUMDB="", GOSUMDB="sum.golang.org",
               GOTOOLCHAIN="local", GOWORK="off", GOFLAGS="",
               GOMODCACHE=str(ROOT / ".cache/mod"), GOCACHE=str(ROOT / ".cache/go"))
    def run(*args, capture=False):
        return subprocess.run(["go", *args], cwd=ROOT, env=env, check=True,
                              stdout=subprocess.PIPE if capture else None, text=True)
    try:
        result = run("list", "-mod=mod", "-m", "-json", "all", capture=True).stdout
        decoder = json.JSONDecoder()
        while result.strip():
            obj, end = decoder.raw_decode(result.lstrip())
            result = result.lstrip()[end:]
            if not obj.get("Main"):
                path = re.sub(r"[A-Z]", lambda m: "!" + m[0].lower(), obj["Path"])
                verify(path, obj["Version"])
        if action == "download":
            run("mod", "download", "all")
        elif action == "tidy":
            run("mod", "tidy")
        records = [{"module": m, "version": v, "timestamp": t} for (m, v), t in sorted(CACHE.items())]
        (ROOT / ".cache").mkdir(exist_ok=True)
        (ROOT / ".cache/dependency-audit.json").write_text(json.dumps({
            "checked_at": NOW.isoformat(), "minimum_age_days": 7, "modules": records,
        }, indent=2) + "\n")
        EVIDENCE_PATH.write_text(json.dumps(dict(sorted(EVIDENCE.items())), indent=2) + "\n")
        print(f"Verified {len(records)} dependency versions: all at least seven days old.")
    finally:
        # Retain only verified index hints, including on a failed check.
        EVIDENCE_PATH.write_text(json.dumps(dict(sorted(EVIDENCE.items())), indent=2) + "\n")
        server.shutdown()


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, urllib.error.URLError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"Dependency check failed: {error}")
