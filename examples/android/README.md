# Android 示例

runvm 不自带测试用的 `.so`——它应该是你自己的库。想先验证链路,golem 仓库里的 `examples/native/native.so` 是一个现成的 AArch64 样本,导出 `add` / `fib` / `slen` / `sum_into` 等函数。

```bash
# 1. 取 golem 的仓库(同时拿到 bionic sysroot)
git clone --depth 1 https://github.com/isesword/golem /tmp/golem

# 2. 指向 sysroot 与 unicorn
export RUNVM_ASSETS=/tmp/golem/assets
export GOLEM_UNICORN=/usr/lib/libunicorn.so.2   # 或 pip 包里的 unicorn/lib/libunicorn.so.2

# 3. 起服务器,调它
./bin/runvm <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"android_open","arguments":{"so":"/tmp/golem/examples/native/native.so"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"android_call","arguments":{"session":"android-1","symbol":"fib","args":[20]}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"android_call","arguments":{"session":"android-1","symbol":"sum_into","args":[{"buffer":4},20,22]}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"android_close","arguments":{"session":"android-1"}}}
EOF
```

预期:`fib(20) = 6765`,`sum_into` 的 `buffers[0].hex` 是 `2a000000`(小端 42)。

换成你自己的库:

```bash
{"name":"android_open","arguments":{"so":"/path/to/libsign.so","dex":"/path/to/classes.dex"}}
```

传 `dex` 之后,guest 触发的 `FindClass` / `GetMethodID` 会按真实的类与方法签名解析,而不是靠你手写的桩。
