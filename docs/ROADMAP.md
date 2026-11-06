# 路线图与已知缺口

按"现在能不能做、做了会不会骗人"排序,而不是按听起来是否带劲。

## 1. H5 截图 / PDF(Linux + Windows 已通,macOS 待做)

**前置条件已达成**:CI 现在在 macOS 与 Windows 上跑 `doctor`,WKWebView 与 WebView2 都能在 runner 里启动并加载页面(macOS 3.6s、Windows 8.4s)。也就是说 fork 写出来之后**有地方能验证**,不必盲写。

**现状**:`h5_screenshot` 在 Linux 上可用——页面开 `visible: true` 画进私有 Xvfb(屏幕=视口),`xwd` 抓根窗口,Go 里解 XWD 转 PNG。截到的一定是那个页面,不是桌面。

**Windows 已通**:不是 fork,是 pid 找错了——窗口由 glaze 建在 `runvm web-host` 子进程里,而抓取在父进程里按自己的 pid 枚举,永远是 0。把宿主 pid 传下去就解决了(诊断过程见 [research/windows-capture.md](research/windows-capture.md))。

**还没有**:macOS 的截图。调研结论:glaze 已经在生产代码里给 WebKit 的 completion handler 传过 Go block(`Find`),所以 ABI 不是问题;真正的工作量是**抓取必须搬进页面宿主进程**,而 webframe 不暴露它的 WebView——两个模块各要一个小改动。方案与 CI 验证清单见 [research/macos-snapshot.md](research/macos-snapshot.md)。

PDF 走"截图 + 嵌入",所以只在能截图的平台上可用(Linux / Windows)。

**为什么难**:webframe 没有截图或打印 API,而它底下的 glaze 启动 WebView 时既不开 CDP 端口也没有 `--remote-debugging-pipe`,所以"在 `web-host` 里接 CDP"这条路根本不通——没有可连的调试端点。三条真正可行的路:

| 方案 | 做法 | 代价 |
|---|---|---|
| A. 给 glaze 加导出 | 在 glaze 的 Linux 后端调 `webkit_web_view_get_snapshot` / `webkit_print_operation_*`,在 macOS 调 `takeSnapshotWithConfiguration:`,Windows 调 `CapturePreview`;再给 webframe 加 `Snapshot()` API | 要维护 glaze 与 webframe 的 fork;三平台三套 API |
| B. Linux X11 抓屏 | 用 `Window: true` 把窗口映射到 Xvfb,再 `xwd`/`import` 抓根窗口 | 只有 Linux;要装 X11 工具;隐藏模式抓不到任何东西 |
| C. 另开一个无头 Chrome | 用已装的 Chrome 对**同一个 URL** 截图 | 抓到的是重放,不是当前会话状态——对"测完再截图"这种需求是假的 |

Linux 选了 B 的变体(私有 Xvfb + xwd + 纯 Go 解码,不需要 ImageMagick)。**macOS/Windows 只能走 A**,要接受 fork 成本;在那之前宁可报"不支持"也不做 C。

PDF 三平台都需要打印 API,glaze 没有暴露,同样卡在 A。

## 2. linux-musl 构建

purego 在无 cgo 时只支持 glibc,golem 与 webframe 都靠它 `dlopen` 系统库。Alpine 需要 `CGO_ENABLED=1` + `zig cc` 交叉编译,一条独立工具链。发布矩阵因此只覆盖 glibc linux / darwin / windows。

## 3. Android:JNI 上行调用(已完成)

~~需要宿主实现 JNI handler~~ → `android_java_stub` / `android_java_stubs` 已上线:按 `class->method` 查表应答 `Call*Method*`,数值桩按调用点强制转换成 int/long/boolean,字符串桩应答对象调用,`null` 表示"对象不存在"。

一个已知约束:golem 把新的类型化平台配置放在 `internal/platform/android`,外部模块只能用**已弃用**的 `emulator.Config.Android.JNI`。代码里注明了原因,等 golem 导出公开构造函数后再迁移。

## 4. 池化后的可观测性(部分完成)

`runvm_status` 现在报每个会话的 `pool` 与 `engines.in_use` / `engines.idle`。

**还缺**:重建次数与 `MaxUses` 回收计数——`PoolStats` 只有 Size/InUse/Idle,要拿到重建次数得在 `newFn` 里自己计数。长跑压测时这是"为什么变慢了"的第一手证据。

## 5. MCP 侧(已完成)

`notifications/cancelled`、HTTP 传输(含 SSE 进度)、会话上限与空闲回收、`h5_wait_for`、
`notifications/progress`、`resources`、`prompts`。

- ~~`notifications/cancelled` 被忽略~~ → 已实现:每个请求的 ctx 注册在服务端,取消通知会真的中断 `android_call`(`pool.Do` 尊重 ctx)和 `h5_eval`。
- ~~会话会泄漏~~ → `RUNVM_MAX_SESSIONS` + `RUNVM_IDLE_TIMEOUT` + 后台回收(按会话 `last_used`)。
- ~~`resources` / `prompts` 是占位~~ → 已实现:3 个资源 + 2 个提示词,且只在有内容时才声明能力。
- ~~没有 progress notification~~ → 已实现,stdio 与 HTTP(SSE)两条都通。
- HTTP 传输没有实现 SSE 的 resumability(事件 id + `Last-Event-ID` 重放):本服务不推消息,这一块暂时不需要。

## 6. 测试(已完成一半)

单元测试覆盖 5 个包(mcp / session / android:args + JNI 桩);`scripts/e2e.py` 会真开 emulator 和 WebView,断言 17 项。

CI 里新增 `e2e` job:装 WebKitGTK + Xvfb + libunicorn(发行版没有就从 Python wheel 取),clone golem 拿 sysroot 和 `native.so`,然后跑同一份脚本。夹具因此不用 vendored 进仓库。
