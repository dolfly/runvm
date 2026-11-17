# AGENTS.md

给在本仓库里干活的编码 Agent（以及新来的人）看的约定。产品说明看 `README.md`，
架构与不变量看 `ARCHITECTURE.md`，现状与缺口看 `docs/roadmap.md`。

## 这个仓库是什么

`runvm` 是一个 MCP 服务器，把「虚拟测试环境」做成模型能直接调用的工具：

- **Android**：用 golem 在本机加载 `.so`，映射真实 bionic，执行 AArch64 指令。不需要 JVM / 真机 / adb。
- **H5**：用 webframe 把页面跑在系统 WebView 里（WebKitGTK / WKWebView / WebView2）。一个页面一个宿主子进程。

一句话原则贯穿全仓库：**不猜、不装**。拿不到的值返回错误，平台没有的能力如实报不支持，不做会骗人的实现。

## 命令

```bash
make build        # 真正的构建，golem 的 unicorn 引擎编进去
make build-plain  # 不带引擎，只做编译检查（android_* 会报不可用）
make test         # 单元测试；不启动 emulator、不启动浏览器
make vet
make fmt          # gofmt -w .
make fmt-check    # CI 用的检查
make doctor       # 真的启动各后端探活
go run ./cmd/runvm selftest   # 平台冒烟：开页面、截图、确认不是白屏、导出 PDF
./scripts/e2e.py  # 完整 e2e（Linux + WebKitGTK + Xvfb + 一个真实 .so）
```

提交前至少跑：`gofmt -l .`、`go vet ./...`、`go test ./...`。
涉及平台代码（`*_darwin.go` / `*_windows.go` / `*_linux.go`）时，还要交叉编译一遍
`GOOS=... GOARCH=... go build -tags unicorn ./cmd/runvm`。

## 目录

| 路径 | 是什么 |
|---|---|
| `cmd/runvm` | 入口：`mcp`（默认）、`http`、`web-host`、`doctor`、`selftest`、`version` |
| `internal/mcp` | JSON-RPC / MCP 协议层，stdio 与 Streamable HTTP 两种传输，进度通知 |
| `internal/tools` | 20 个工具、4 个资源、2 个提示词。**工具描述就是给模型的文档**，改动要当成 API 改动来对待 |
| `internal/h5` | 页面宿主：子进程协议（`protocol.go`）、会话（`session.go`）、宿主主体（`host.go`）、截图与 PDF 的平台实现 |
| `internal/android` | `.so` 会话、ELF 导出表、JNI 桩、engine pool |
| `internal/session` | 会话存储与空闲回收 |
| `internal/doctor` | 探活 |
| `docs/` | 发布在 GitHub Pages 上的站点（`_config.yml`），中文 |
| `scripts/` | `release.sh`（交叉编译矩阵）、`e2e.py`（端到端） |

## 不变量（改代码时要守住的）

摘自 `ARCHITECTURE.md` §6，违反任何一条都算 bug：

1. **stdout 只走协议。** 任何日志进 stderr。子进程的 stdout 只能是被解析的 JSON。
2. **一个会话一把锁。** golem 的 emulator 不允许并发调用；同一页面的 `h5_eval` 串行（语句顺序是 API 的一部分），不同页面的调用可以并行。
3. **不重复关闭。** `ready`/`done` 用 `sync.Once`；`Close()` 幂等。
4. **超时必须能收敛。** 每个等待都有上限（ready 30s、eval 30s、close 3s、工具 120s、PDF 打印 60s）。
5. **不猜。** 拿不到的值返回错误，不返回零值假装成功。

另外两条来自实现细节、踩过坑的约定：

- **平台能力用旁路接口表达。** 不要在 `glaze.WebView` 那种公共接口上加方法——没实现的平台会直接编译不过。用 `Snapshotter` / `PDFPrinter` 这类可选接口 + 类型断言，不支持就返回 `ErrXxxUnsupported` 让调用方如实回落。
- **跨进程的东西要传 pid/句柄，别按自己的身份找。** Windows 截图踩过：窗口在页面宿主子进程里，父进程按自己的 pid 枚举永远找不到。

## 依赖与两个 fork

`go.mod` 里有**一条 `replace`**：`tractor.dev/webframe => github.com/dolfly/webframe-fork`。
Go 会忽略依赖里的 `replace`，所以本模块必须重复一遍。链路是：

```
runvm  →(replace)  dolfly/webframe-fork  →(require)  dolfly/glaze
```

两个 fork 都按 **tag** 钉住，新增能力必须按顺序发版，否则 `go.mod` 解析不到：

1. `dolfly/glaze`：在 `dist` 分支上改（该分支声明的模块路径是 `github.com/dolfly/glaze`），推 `dist`，打 tag。macOS 原生能力（`Snapshot` / `PrintPDF`）在这一层实现，测试用 `go test` 跑真实 WKWebView（`TestMain` 里注册 scenario）。
2. `dolfly/webframe-fork`：`Frame` 的 glaze 字段是未导出的，新能力要在这一层透出（`Frame.Snapshot` / `Frame.PrintPDF`），升 glaze 版本、推 `main`、打 tag。
3. `runvm`：升 `go.mod` 的 `replace` 版本 + glaze 的 indirect 版本，然后才在 `internal/h5` 里用。

不能只改一个仓库就宣称完成——中间那一层不改，能力就上不来。

## 文档同步

这个仓库把「会不会骗人」当第一原则，所以**能力变了，文档必须同一 PR 里改**：

- `docs/roadmap.md` 的「已具备 / 还没做」两张表；
- `docs/index.md` 的平台支持表；
- `README.md` 的「限制，以及它不是」；
- `docs/tools.md` 的工具表，以及 `internal/tools/tools.go` 里的 `Description` 字符串（模型读的是这一句）。

写文档时说清「哪个平台、哪条路径、失败时是什么样」，不要写「都支持」。

## 提交与 PR

- commit message：**英文**，`区域: 一句话` 前缀，例如 `pdf: print a vector PDF on macOS`、`ci: stop unattended-upgrades…`。
  正文讲「为什么」，不要复述 diff。
- 中文留给文档和注释里的取舍说明；代码注释用英文。
- 不写 `Generated with …` 之类的署名尾巴。
- PR 标题带 Multica issue key（如 `DO-32: …`），平台靠它把 PR 关联到看板上的 issue；分支名同理。
- 一个 issue 一个 PR 是默认；要拆就按「能独立合并、能独立验证」拆，不要按前后端分层拆。
- 改动要聚焦：不顺手重构、不跑全仓库格式化。

## 刻意不做的事

见 `ARCHITECTURE.md` §7，别好心加回来：

- 不做通用 RPC（MCP 是唯一入口）；
- 不做 CDP 注入（截图/PDF 走 WebView 自己的 API）；
- 不做容器编排（一个 runvm 进程就是一批评测的全部）；
- 不做安全沙箱（要隔离就上容器和独立用户，而不是假装这层是边界）。

## 环境

- Go 版本见 `go.mod`。`CGO_ENABLED=0` 是默认：webframe 与 golem 都用 purego 在运行时加载系统库，所以不需要 C 工具链，交叉编译即可。
- Android 后端需要 golem 的 bionic sysroot（`RUNVM_ASSETS` 可覆盖路径）和 `libunicorn`。
- Linux 的截图需要 `xwd`（`x11-apps`），页面要在自己起的 Xvfb 里画（`visible: true` 且不继承外部 `DISPLAY`）。
- 不要把密钥、令牌写进仓库或提交信息。
