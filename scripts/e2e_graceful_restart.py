#!/usr/bin/env python3
"""End-to-end regression for DO-127 B-1: a graceful restart must keep the
session ledger.

Unit tests cover the Set mechanics, but the B-1 regression lived in the
cmd/runvm wiring (the shutdown defer), which no unit test sees. This script
drives the real binary over stdio MCP with a real WebKit page:

    open(profile) -> SIGTERM -> ledger still records the session
    restart -> runvm_status reports it stale with reason + reopen
    reopen the same profile -> restored:true under the SAME id, cookie alive
    second graceful restart -> the next session gets a FRESH id (no reuse)
    runvm_close_all still wipes the ledger at once (explicit-close semantics),
    and a restart after close_all resurrects nothing

Exits non-zero if any expectation fails. On pre-fix code the very first
post-SIGTERM assertion fails (the ledger was rewritten to {"sessions": []}).
"""

import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

BIN = os.environ.get("RUNVM_BIN", "./bin/runvm")
COOKIE = "to127e2e=persist"
FAILED = []


def expect(label, ok, detail=""):
    print(f"{'ok  ' if ok else 'FAIL'} {label}" + (f": {detail[:200]}" if detail else ""), flush=True)
    if not ok:
        FAILED.append(label)


class Fixture(BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"<html><body>graceful-restart-fixture</body></html>"
        self.send_response(200)
        self.send_header("Content-Type", "text/html")
        self.send_header("Set-Cookie", COOKIE + "; Max-Age=3600; Path=/")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *a):
        pass


class Client:
    def __init__(self, data_dir):
        env = dict(os.environ)
        env["RUNVM_DATA_DIR"] = data_dir
        self.p = subprocess.Popen(
            [BIN], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL, text=True, bufsize=1, env=env,
        )
        self.id = 0

    def call(self, name, **args):
        self.id += 1
        msg = {"jsonrpc": "2.0", "id": self.id, "method": "tools/call",
               "params": {"name": name, "arguments": args}}
        self.p.stdin.write(json.dumps(msg) + "\n")
        self.p.stdin.flush()
        while True:
            line = self.p.stdout.readline()
            if not line:
                raise SystemExit(f"server closed stdout during {name}")
            r = json.loads(line)
            if r.get("id") != self.id:
                continue
            content = r.get("result", {}).get("content", [])
            if content and content[0].get("type") == "text":
                try:
                    return json.loads(content[0]["text"])
                except json.JSONDecodeError:
                    return {"_raw": content[0]["text"]}
            return r.get("result", {})

    def sigterm(self):
        self.p.send_signal(signal.SIGTERM)
        try:
            self.p.stdin.close()  # unblock the stdio reader so shutdown defers run
        except Exception:
            pass
        self.p.wait(timeout=60)


def read_ledger(data_dir):
    path = os.path.join(data_dir, "sessions.json")
    if not os.path.exists(path):
        return None
    with open(path) as f:
        return json.load(f)


def ledger_ids(data_dir):
    led = read_ledger(data_dir)
    if not led:
        return []
    return [s.get("id") for s in led.get("sessions", [])]


def main():
    fixture = HTTPServer(("127.0.0.1", 0), Fixture)
    threading.Thread(target=fixture.serve_forever, daemon=True).start()
    url = f"http://127.0.0.1:{fixture.server_address[1]}/"

    data_dir = tempfile.mkdtemp(prefix="runvm-e2e-graceful-")
    try:
        # 1) open a persistent-profile session; the ledger must record it
        m = Client(data_dir)
        r1 = m.call("h5_open", target=url, ephemeral=False, profile="e2e127")
        expect("open #1 gets h5-1", r1.get("session") == "h5-1", json.dumps(r1))
        time.sleep(1.0)
        expect("ledger records h5-1 while live", "h5-1" in ledger_ids(data_dir),
               str(ledger_ids(data_dir)))

        # 2) graceful stop: the ledger must NOT be rewritten empty (B-1)
        m.sigterm()
        ids = ledger_ids(data_dir)
        expect("ledger survives SIGTERM", "h5-1" in ids, str(ids))

        # 3) restart: the record comes back stale with reason + reopen args
        m2 = Client(data_dir)
        st = m2.call("runvm_status")
        sessions = st.get("persistence", {}).get("sessions", [])
        e = next((s for s in sessions if s.get("session") == "h5-1"), None)
        expect("h5-1 reported stale with reason + reopen",
               bool(e and e.get("state") == "stale" and e.get("reason")
                    and e.get("reopen", {}).get("profile") == "e2e127"),
               json.dumps(e))

        # 4) reopening the same profile restores under the same id, cookie alive
        r2 = m2.call("h5_open", target=url, ephemeral=False, profile="e2e127")
        expect("reopen restores with the same id",
               r2.get("restored") is True and r2.get("session") == "h5-1", json.dumps(r2))
        ck = m2.call("h5_eval", session="h5-1", js="document.cookie")
        expect("profile cookie survives the restart", COOKIE in json.dumps(ck), json.dumps(ck))

        # 5) second graceful restart: ids continue, no reuse
        m2.sigterm()
        m3 = Client(data_dir)
        r3 = m3.call("h5_open", target=url)
        expect("next id after graceful restarts is not reused",
               r3.get("session") == "h5-2", json.dumps(r3))

        # 6) explicit close_all still wipes the ledger at once (only h5-2 is
        # live here; h5-1 is a stale record), and nothing comes back after that
        rc = m3.call("runvm_close_all", backends=["h5"])
        expect("runvm_close_all wipes the ledger immediately",
               rc.get("count") == 1 and ledger_ids(data_dir) == [],
               f"close_all={json.dumps(rc)} ledger={ledger_ids(data_dir)}")
        m3.sigterm()
        m4 = Client(data_dir)
        st4 = m4.call("runvm_status")
        expect("nothing resurrected after close_all + graceful restart",
               st4.get("persistence", {}).get("sessions", []) == [],
               json.dumps(st4.get("persistence", {}))[:200])
        m4.sigterm()
    finally:
        shutil.rmtree(data_dir, ignore_errors=True)
        fixture.shutdown()

    if FAILED:
        print(f"\n{len(FAILED)} expectation(s) failed: {FAILED}")
        return 1
    print("\ngraceful-restart e2e: all expectations passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
