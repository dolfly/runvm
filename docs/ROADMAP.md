# 路线图与已知缺口

按"现在能不能做、做了会不会骗人"排序,而不是按听起来是否带劲。

## 1. H5 截图 / PDF(阻塞,不是没写)

**现状**:`h5_open` / `h5_eval` 能驱动页面并断言,但拿不到像素。

**为什么难**:webframe 没有截图或打印 API,而它底下的 glaze 启动 WebView 时既不开 CDP 端口也没有 `--remote-debugging-pipe`,所以"在 `web-host` 里接 CDP"这条路根本不通——没有可连的调试端点。三条真正可行的路:

| 方案 | 做法 | 代价 |
|---|---|---|
| A. 给 glaze 加导出 | 在 glaze 的 Linux 后端调 `webkit_web_view_get_snapshot` / `webkit_print_operation_*`,在 macOS 调 `takeSnapshotWithConfiguration:`,Windows 调 `CapturePreview`;再给 webframe 加 `Snapshot()` API | 要维护 glaze 与 webframe 的 fork;三平台三套 API |
| B. Linux X11 抓屏 | 用 `Window: true` 把窗口映射到 Xvfb,再 `xwd`/`import` 抓根窗口 | 只有 Linux;要装 X11 工具;隐藏模式抓不到任何东西 |
| C. 另开一个无头 Chrome | 用已装的 Chrome 对**同一个 URL** 截图 | 抓到的是重放,不是当前会话状态——对"测完再截图"这种需求是假的 |

**倾向 A**,但要接受 fork 成本;在那之前宁可报错也不做 C。

## 2. linux-musl 构建

purego 在无 cgo 时只支持 glibc,golem 与 webframe 都靠它 `dlopen` 系统库。Alpine 需要 `CGO_ENABLED=1` + `zig cc` 交叉编译,一条独立工具链。发布矩阵因此只覆盖 glibc linux / darwin / windows。

## 3. Android:JNI 上行调用

`android_open` 已经能传 `dex`,让 `FindClass` / `GetMethodID` 按真实签名解析;但 guest 反向调 Java(比如 `CallStaticObjectMethod` 拿一个 `Context`)还需要宿主提供实现。golem 的 JNI handler 是 Go 接口,runvm 目前没有把它暴露成工具。

**做法**:加 `android_java_stub` 工具(`{class, method, returns}`),在 `Config` 的 JNI handler 里查表并返回默认值。这样"库依赖 Android 框架"的场景才真正可测。

## 4. 池化后的故障语义

`emulator.Pool` 会在 `MaxUses` 之后回收引擎、在 panic 后重建。runvm 目前只是透传——一个引擎崩了,下一个请求拿到新引擎继续。

**缺的是可观测性**:`runvm_status` 应该报每个会话的 `InUse` / `Idle` / 重建次数,否则长跑压测时"为什么变慢了"只能猜。

## 5. MCP 侧

- `notifications/cancelled` 现在被忽略:取消不会中断已经在跑的 `android_call`。要实现得给 `pool.Do` 传可取消的 ctx(已经传了)并在工具层暴露取消信号。
- `resources` / `prompts` 返回空数组,占位而已。
- 没有 progress notification:`h5_open` 冷启动 1.5 秒、`android_open` 池化 4 引擎会更久,这些过程对模型是不可见的。

## 6. 测试

现在有 4 个包的单元测试,以及 `scripts/e2e.py`(真开 emulator 和 WebView,断言 9 项)。缺的是:

- **CI 里跑真后端**:ubuntu runner 上装 WebKitGTK/Xvfb/libunicorn 后跑一次 `h5_open` + `android_open`,能挡住"本地能跑、干净环境跑不了"这类问题。
- **回归夹具**:一个 10 KB 的 AArch64 `.so`(golem 的 `examples/native/native.so`)不适合直接 vendored 进仓库,可以在 CI 里 clone golem 取用。
