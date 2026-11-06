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

## 3. Android:JNI 上行调用(已完成)

~~需要宿主实现 JNI handler~~ → `android_java_stub` / `android_java_stubs` 已上线:按 `class->method` 查表应答 `Call*Method*`,数值桩按调用点强制转换成 int/long/boolean,字符串桩应答对象调用,`null` 表示"对象不存在"。

一个已知约束:golem 把新的类型化平台配置放在 `internal/platform/android`,外部模块只能用**已弃用**的 `emulator.Config.Android.JNI`。代码里注明了原因,等 golem 导出公开构造函数后再迁移。

## 4. 池化后的可观测性(部分完成)

`runvm_status` 现在报每个会话的 `pool` 与 `engines.in_use` / `engines.idle`。

**还缺**:重建次数与 `MaxUses` 回收计数——`PoolStats` 只有 Size/InUse/Idle,要拿到重建次数得在 `newFn` 里自己计数。长跑压测时这是"为什么变慢了"的第一手证据。

## 5. MCP 侧

- `notifications/cancelled` 现在被忽略:取消不会中断已经在跑的 `android_call`。要实现得给 `pool.Do` 传可取消的 ctx(已经传了)并在工具层暴露取消信号。
- `resources` / `prompts` 返回空数组,占位而已。
- 没有 progress notification:`h5_open` 冷启动 1.5 秒、`android_open` 池化 4 引擎会更久,这些过程对模型是不可见的。

## 6. 测试(已完成一半)

单元测试覆盖 5 个包(mcp / session / android:args + JNI 桩);`scripts/e2e.py` 会真开 emulator 和 WebView,断言 17 项。

CI 里新增 `e2e` job:装 WebKitGTK + Xvfb + libunicorn(发行版没有就从 Python wheel 取),clone golem 拿 sysroot 和 `native.so`,然后跑同一份脚本。夹具因此不用 vendored 进仓库。
