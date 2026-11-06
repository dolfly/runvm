# runvm

把「虚拟测试环境」做成模型能直接调用的工具:一套 MCP 服务器,两个后端。

- **Android**:用 [golem](https://github.com/isesword/golem) 在本机加载一个 AArch64 的 `.so`,跑它的 `init_array` / `JNI_OnLoad`,然后把导出函数直接调用起来——不需要 JVM、不需要真机、不需要 adb。
- **H5**:用 [webframe](https://github.com/tractordev/webframe) 把页面跑在系统自带的 WebKit 里,可以执行任意 JS、点击、断言,拿回真实的 DOM 结果。

模型不再"读代码猜行为":它能开一个环境、调一个符号、跑一段 JS、读出结果。

```
$ echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"android_call",
  "arguments":{"session":"android-1","symbol":"fib","args":[20]}}}' | runvm
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\n  \"hex\": \"0x1a6d\",\n  \"i32\": 6765,\n  \"symbol\": \"fib\",\n  \"value\": 6765\n}"}]}}
```

## 架构

```mermaid
flowchart TB
  M["模型 / MCP 客户端"] -->|"JSON-RPC 2.0 over stdio"| S["runvm mcp<br/>9 个工具"]
  S --> REG["session.Store<br/>android-N / h5-N"]
  REG --> A["android 后端<br/>in-process"]
  REG --> H["h5 后端<br/>进程外"]
  A --> G["golem emulator<br/>bionic libc + unicorn(purego)"]
  G --> SO[("libfoo.so<br/>AArch64")]
  H -->|"JSONL 管道"| W["runvm web-host<br/>webframe.Frame"]
  W --> K["系统 WebView<br/>WebKitGTK / WKWebView / WebView2"]
```

三层职责分明:

| 层 | 位置 | 职责 |
|---|---|---|
| MCP 传输 | `internal/mcp` | JSON-RPC 2.0、`initialize`/`tools/list`/`tools/call`,零依赖 |
| 工具与会话 | `internal/tools`、`internal/session` | 参数校验、会话 ID、生命周期 |
| 后端 | `internal/android`、`internal/h5` | 真正加载 .so / 真正跑页面 |

## 为什么是这两个后端,以及为什么这样接

**Android 用 golem 而不是 adb/模拟器**:golem 是纯 Go 的 native 库模拟框架,把 bionic(`libc/libm/libdl`)和目标 `.so` 一起映射进一块虚拟地址空间,指令由 unicorn 执行。产物是单个二进制、`CGO_ENABLED=0`,可以在 CI 容器里跑签名/加密这类"必须调 native 才知道结果"的逻辑。runvm 只做会话层:一个会话一个 emulator,串行化调用(golem 的 emulator 不是并发安全的),并发靠多开会话。

**H5 用子进程而不是同进程**:webframe 的 `Frame` 必须建在主 goroutine 上,`Run()` 会一直阻塞它——一个页面独占一个进程。而 MCP 服务器要长期存活并服务多个会话,两者无法共存于一个主线程。所以每个页面是一个 `runvm web-host` 子进程,父子之间走一行一个 JSON 的管道:控制流清晰,页面崩了也不会带走 MCP 服务器。

`web-host` 里补了两件 webframe 库不做、但库的调用方必须自己做的事(它的 CLI 里做了):无 `DISPLAY` 时拉起私有 Xvfb,以及把本地路径转成 `file://` URL。

## 快速开始

要求:Go 1.27+;Linux 额外需要 WebKitGTK 和 Xvfb(Android 后端需要 libunicorn)。

```bash
# 1. 编译(用 -tags unicorn 才会编入 CPU 引擎)
make build            # -> bin/runvm

# 2. 自检:会真的启动一个 emulator 和一个 WebView
./bin/runvm doctor
#   ✓ bionic sysroot  /path/to/golem/assets
#   ✓ cpu engine      booted bionic in 42ms
#   ✓ webview         about:blank ready in 812ms

# 3. 接到 MCP 客户端
```

```json
{
  "mcpServers": {
    "runvm": {
      "command": "/abs/path/bin/runvm",
      "args": [],
      "env": {
        "RUNVM_ASSETS": "/path/to/golem/assets",
        "GOLEM_UNICORN": "/usr/lib/libunicorn.so.2"
      }
    }
  }
}
```

Linux 依赖:`apt install libwebkit2gtk-4.1-0 xvfb libgl1-mesa-dri`;Unicorn 在 Debian/Ubuntu 上是 `libunicorn2`,若发行版没有,可以从 Python wheel 里取(`pip install unicorn` 后 `unicorn/lib/libunicorn.so.2`)。

## MCP 工具

| 工具 | 作用 |
|---|---|
| `runvm_status` | 两个后端是否可用、有几个会话(**不启动任何东西**) |
| `android_open` | 加载 `.so`,跑 init,返回 `android-N` |
| `android_symbols` | 列出导出函数(直接读 ELF,不依赖会话能否启动) |
| `android_call` | 调一个导出函数,返回 `value`/`hex`/`i32` |
| `android_close` | 释放 emulator |
| `h5_open` | 在 WebKit 里打开 URL / 本地文件 / `about:blank` |
| `h5_eval` | 在页面里执行 JS,返回结果(自动 await Promise) |
| `h5_console` | 取回缓冲的 console 与页面异常 |
| `h5_close` | 关闭页面宿主进程 |

参数细节见 [docs/mcp.md](docs/mcp.md)。`android_call` 的 `args` 支持三种形态,一条调用就能覆盖大多数 native 接口:

```jsonc
{"args": [2, 3]}                    // 整数参数
{"args": ["hello"]}                 // char*,自动拷进 guest 内存
{"args": [{"buffer": 64}, 20]}      // 出参:64 字节缓冲区,调用后读回 hex/ascii
```

## 环境变量

| 变量 | 作用 |
|---|---|
| `RUNVM_ASSETS` | golem 的 bionic sysroot(`assets/android/sdk23`);不设则用 golem 模块内路径 |
| `RUNVM_ENGINE` | `android_open` 未指定 `engine` 时的默认后端 |
| `GOLEM_UNICORN` | 显式指定 `libunicorn` 路径(golem 读取) |

## 限制,以及它**不是**什么

- **不是安全沙箱**。加载的 `.so` 在你的进程里被模拟执行,页面在真实 WebKit 里运行。只跑你信任的代码。
- **一个页面一个进程**,`h5_open` 的成本约 0.5–1.5 秒;批量测试请复用会话。
- **没有截图/PDF/网络拦截**。webframe 的 API 不提供,需要的话应该在 `web-host` 里加 CDP 或注入脚本,而不是假装有。
- **macOS / Windows 未实测**。代码路径按 webframe/golem 的支持写好了(macOS 无需 Xvfb,Windows 需要 WebView2),但没有 CI 覆盖。
- **`go.mod` 里有一条 `replace`**。webframe 把 glaze 钉在作者的 fork 上,而 Go 会忽略依赖里的 `replace`,所以本模块必须重复一遍;升级 webframe 时这里要跟着改。

## 开发

```bash
make build     # 带 unicorn 引擎
make test      # 单元测试(不启动 emulator/浏览器)
make vet
make fmt
./bin/runvm doctor
```

代码结构:

```
cmd/runvm/        入口:mcp / doctor / web-host / version
internal/mcp/     JSON-RPC 2.0 与工具注册(零依赖)
internal/tools/   9 个工具的参数校验与调用编排
internal/session/ 会话注册表(泛型)
internal/android/ golem 会话 + ELF 导出表
internal/h5/      webframe 宿主(子进程)与驱动
internal/doctor/  真实探测两个后端
```

## 依赖与致谢

- [golem](https://github.com/isesword/golem)(Apache-2.0):纯 Go 的 native 库模拟框架,runvm 的 Android 后端是它的一层封装。
- [webframe](https://github.com/tractordev/webframe)(MIT):把网页当程序跑,runvm 的 H5 后端。
- [glaze](https://github.com/progrium/glaze)(MIT):webframe 的纯 Go WebView 绑定,靠 purego 在运行时 `dlopen` 系统 WebView。

MIT License。
