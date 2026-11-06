#!/usr/bin/env python3
#!/usr/bin/env python3
"""End-to-end check against a live runvm binary.

Usage:
    ./bin/runvm &   # no: this drives it itself
    python3 scripts/e2e.py /path/to/libfoo.so

Sends one request and waits for its reply, so dependent calls stay ordered,
while the two h5_open calls are pipelined on purpose to show that independent
sessions really do overlap. Exits non-zero if any expectation fails.
"""

import json
import os
import subprocess
import sys
import time
import json
import subprocess
import sys
import time

BIN = os.environ.get("RUNVM_BIN", "./bin/runvm")
FAILED = []


def expect(label, resp, want):
    got = text(resp)
    ok = want in got
    print(f"{'ok  ' if ok else 'FAIL'} {label}: {got[:120].replace(chr(10), ' ')}")
    if not ok:
        FAILED.append(f"{label}: want {want!r} in {got[:200]!r}")


class Client:
    def __init__(self):
        self.p = subprocess.Popen(
            [BIN], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL, text=True, bufsize=1,
        )
        self.id = 0

    def send(self, method, params=None):
        self.id += 1
        msg = {"jsonrpc": "2.0", "id": self.id, "method": method}
        if params is not None:
            msg["params"] = params
        self.p.stdin.write(json.dumps(msg) + "\n")
        self.p.stdin.flush()
        return self.id

    def recv(self):
        line = self.p.stdout.readline()
        if not line:
            raise SystemExit("server closed stdout")
        return json.loads(line)

    def call(self, name, **args):
        self.send("tools/call", {"name": name, "arguments": args})
        return self.recv()

    def close(self):
        self.p.stdin.close()
        self.p.wait(timeout=15)


def text(resp):
    r = resp.get("result") or {}
    if "content" in r:
        return r["content"][0]["text"]
    return json.dumps(resp)


def show(label, resp):
    print(f"{label}: {text(resp)[:180]}")


def main():
    if len(sys.argv) < 2:
        raise SystemExit("usage: python3 scripts/e2e.py /path/to/libfoo.so")
    c = Client()
    c.send("initialize", {"protocolVersion": "2025-06-18"})
    c.recv()

    print("== android: pool + hook ==")
    expect("open(pool=2)", c.call("android_open", so=sys.argv[1], pool=2), '"pool": 2')
    expect("fib(20)", c.call("android_call", session="android-1", symbol="fib", args=[20]), '"value": 6765')
    expect("hook fib->42", c.call("android_hook", session="android-1", symbol="fib", returns=42), '"returns": 42')
    expect("fib after hook", c.call("android_call", session="android-1", symbol="fib", args=[20]), '"value": 42')
    expect("add(2,3) untouched", c.call("android_call", session="android-1", symbol="add", args=[2, 3]), '"value": 5')
    expect("java stub (long)", c.call("android_java_stub", session="android-1",
                                      **{"class": "java/lang/System", "method": "currentTimeMillis",
                                         "returns": 1700000000000}), "currentTimeMillis")
    expect("java stub (string)", c.call("android_java_stub", session="android-1",
                                        **{"class": "android.content.Context", "method": "getPackageName",
                                           "returns": "com.example.app"}), "com.example.app")
    expect("java stubs listed", c.call("android_java_stubs", session="android-1"), "getPackageName")
    expect("duplicate stub refused", c.call("android_java_stub", session="android-1",
                                            **{"class": "java/lang/System", "method": "currentTimeMillis",
                                               "returns": 1}), "already registered")
    expect("pool stats", c.call("runvm_status"), '"engines"')
    expect("limits reported", c.call("runvm_status"), '"max_sessions"')
    expect("re-hook refused", c.call("android_hook", session="android-1", symbol="fib", returns=7), "already stubbed")
    expect("close", c.call("android_close", session="android-1"), '"closed"')

    print("== h5: pipelined opens and non-blocking dispatch ==")
    t0 = time.time()
    first = c.call("h5_open", target="examples/h5/page.html")
    single = time.time() - t0
    sid1 = json.loads(text(first))["session"]

    # Four opens in flight at once: all must succeed, and none may be dropped.
    for _ in range(3):
        c.send("tools/call", {"name": "h5_open", "arguments": {"target": "examples/h5/page.html"}})
    replies = [c.recv() for _ in range(3)]
    total = time.time() - t0
    sids = [sid1] + [json.loads(text(r))["session"] for r in replies]
    ok = len(set(sids)) == 4
    print(f"{'ok  ' if ok else 'FAIL'} four pages open ({total:.2f}s, one load {single:.2f}s): {sids}")
    if not ok:
        FAILED.append(f"pipelined opens returned duplicate sessions: {sids}")

    # A slow call must not block an unrelated one. h5_wait_for with a 2.5s
    # timeout is the slow call; runvm_status has to answer while it runs.
    slow = c.send("tools/call", {"name": "h5_wait_for",
                                 "arguments": {"session": sids[0], "expression": "false", "timeout_ms": 2500}})
    t1 = time.time()
    fast = c.send("tools/call", {"name": "runvm_status", "arguments": {}})
    first_reply = c.recv()
    reply_latency = time.time() - t1
    ok = first_reply.get("id") == fast and reply_latency < 1.5
    print(f"{'ok  ' if ok else 'FAIL'} status answered in {reply_latency:.2f}s "
          f"while a 2.5s call was in flight")
    if not ok:
        FAILED.append(f"dispatch blocked: got reply id {first_reply.get('id')} after {reply_latency:.2f}s")
    slow_reply = c.recv()
    if slow_reply.get("id") != slow:
        FAILED.append(f"slow call replied with id {slow_reply.get('id')}, want {slow}")

    for sid in sids:
        expect(f"title {sid}", c.call("h5_eval", session=sid, js="document.title"), "runvm demo")
    for sid in sids:
        expect(f"close {sid}", c.call("h5_close", session=sid), '"closed"')

    print("== h5: wait_for ==")
    opened = c.call("h5_open", target="examples/h5/page.html")
    sid = json.loads(text(opened))["session"]
    c.call("h5_eval", session=sid, js="setTimeout(function () { document.body.dataset.ready = '1' }, 300)")
    expect("wait_for selector", c.call("h5_wait_for", session=sid, selector="[data-ready]", timeout_ms=5000), '"matched": true')
    expect("wait_for timeout", c.call("h5_wait_for", session=sid, expression="false", timeout_ms=300), "timed out")
    expect("close", c.call("h5_close", session=sid), '"closed"')

    print("== h5: screenshot ==")
    shot_path = "e2e-screenshot.png"
    opened = c.call("h5_open", target="examples/h5/page.html", visible=True)
    sid = json.loads(text(opened))["session"]
    shot = c.call("h5_screenshot", session=sid, path=shot_path)
    content = (shot.get("result") or {}).get("content") or []
    image = next((x for x in content if x.get("type") == "image"), None)
    ok = image is not None and image.get("mimeType") == "image/png" and len(image.get("data", "")) > 1000
    print(f"{'ok  ' if ok else 'FAIL'} screenshot returns an image: {[x.get('type') for x in content]}")
    if not ok:
        FAILED.append(f"screenshot content: {content}")
    on_disk = os.path.exists(shot_path) and open(shot_path, "rb").read(8) == b"\x89PNG\r\n\x1a\n"
    print(f"{'ok  ' if on_disk else 'FAIL'} screenshot written to {shot_path}")
    if not on_disk:
        FAILED.append("screenshot file missing or not a PNG")
    if os.path.exists(shot_path):
        os.remove(shot_path)
    expect("close visible", c.call("h5_close", session=sid), '"closed"')

    c.close()

    if FAILED:
        print("\nfailures:")
        for f in FAILED:
            print("  -", f)
        raise SystemExit(1)
    print("\nall checks passed")


if __name__ == "__main__":
    main()
