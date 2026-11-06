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
    expect("re-hook refused", c.call("android_hook", session="android-1", symbol="fib", returns=7), "already stubbed")
    expect("close", c.call("android_close", session="android-1"), '"closed"')

    print("== h5: two pages, pipelined opens ==")
    start = time.time()
    c.send("tools/call", {"name": "h5_open", "arguments": {"target": "examples/h5/page.html"}})
    c.send("tools/call", {"name": "h5_open", "arguments": {"target": "examples/h5/page.html"}})
    a, b = c.recv(), c.recv()
    elapsed = time.time() - start
    ok = elapsed < 2.8
    print(f"{'ok  ' if ok else 'FAIL'} two opens in {elapsed:.2f}s (one page load is ~1.5s)")
    if not ok:
        FAILED.append(f"two h5_open calls took {elapsed:.2f}s: they serialized instead of overlapping")
    expect("page 1 title", c.call("h5_eval", session="h5-1", js="document.title"), "runvm demo")
    expect("page 2 title", c.call("h5_eval", session="h5-2", js="document.title"), "runvm demo")
    expect("close 1", c.call("h5_close", session="h5-1"), '"closed"')
    expect("close 2", c.call("h5_close", session="h5-2"), '"closed"')

    c.close()

    if FAILED:
        print("\nfailures:")
        for f in FAILED:
            print("  -", f)
        raise SystemExit(1)
    print("\nall checks passed")


if __name__ == "__main__":
    main()
