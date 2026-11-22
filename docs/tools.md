---
title: 工具参考
layout: page
---

# 工具参考

传输是 JSON-RPC 2.0：stdio 一行一条消息；HTTP 走 `POST /mcp`。协议版本 `2025-06-18`（兼容 `2025-03-26`、`2024-11-05`）。

约定：工具执行失败返回 `result.isError = true` 与一段可读文本（模型能读、能自己修）；协议层错误才用 JSON-RPC error。单次调用上限 120 秒。会话 ID 形如 `android-1`、`ios-2`、`h5-3`。

## 通用

| 工具 | 作用 |
|---|---|
| `runvm_status` | 后端可用性、限制、活跃会话。不启动任何东西 |
| `runvm_upload` | 把本机文件上传进可写资源空间（`runvm://files/`），返回 URI / 字节数 / sha256 |
| `runvm_write` | 把文本或 base64 二进制内容写/覆盖到一个可写资源，返回 URI / 字节数 / sha256 |

**可写资源与失败语义**：`runvm_upload` 的参数是 `path`（本机文件路径，必填）+ `target`（可选相对路径/名字，默认取源文件 base name）；`runvm_write` 的参数是 `target`（必填）+ `content`（文本）或 `base64`（二进制），两者二选一。两者都返回 `{"uri", "bytes", "sha256", "name"}`，不是一句 "ok"。

失败时是 `result.isError = true` 的可读错误，不假装成功：

- 路径穿越（`..`、绝对路径、符号链接逃出资源根）→ `resource path escapes the resources root`；
- 超限 → `resource exceeds the size limit`（默认 64MiB，可配 `RUNVM_RESOURCES_MAX_SIZE`）；
- 资源存储未配置 → `resource store is not configured`。

写入落在资源根内，目录 0700、文件 0600；空文件与二进制文件都按原样处理。

## Android

| 工具 | 关键参数 | 作用 |
|---|---|---|
| `android_open` | `so`、`engine`、`dex`、`pool`、`max_uses` | 加载 `.so`，跑 init，返回会话 |
| `android_symbols` | `session`、`filter`、`limit` | 列导出函数（直接读 ELF） |
| `android_hook` | `session`、`symbol`、`returns` | 把函数换成返回常量的桩 |
| `android_java_stub` | `session`、`class`、`method`、`returns` | 应答 JNI 上行调用 |
| `android_java_stubs` | `session` | 列出已注册的 Java 桩 |
| `android_call` | `session`、`symbol`、`args` | 调导出函数 |
| `android_close` | `session` | 释放 emulator |

`android_call` 的参数：整数、字符串（`char*`）、`{"buffer": n}`（出参，调用后读回 hex/ascii），最多 8 个。负数按补码传。

`android_open` 可以开**引擎池**（`"pool": 4`）：多个 AArch64 引擎共享 `.so` 的只读页，同一会话的调用可并行。默认 1。

`android_open` 按**容器格式**分流：传进来的文件是 Mach-O（dylib）时，它不会去碰 bionic sysroot 和 JNI，而是按下面的 iOS 语义打开并返回 `ios-N` 会话，结果里带 `routed_to: "ios"`。

## iOS

| 工具 | 关键参数 | 作用 |
|---|---|---|
| `ios_open` | `dylib`、`engine`、`pool`、`max_uses` | 加载 Mach-O arm64 dylib，跑 init，返回会话（含 `system_libs`：`LC_LOAD_DYLIB` 声明的依赖） |
| `ios_symbols` | `session`、`filter`、`limit` | 列导出符号与导入符号（读磁盘上的 Mach-O，但仍要会话 id 定位文件） |
| `ios_imports` | `session` | 列 dylib 需要的外部符号 + 声明的依赖库——这些都没有实现 |
| `ios_call` | `session`、`symbol`、`args` | 调导出函数 |
| `ios_close` | `session` | 释放 ios 会话 |

`ios_call` 的 `args` 与 `android_call` 相同：整数、字符串（`char*`）、`{"buffer": n}`，最多 8 个。符号名**不带前导下划线**（Mach-O 的 `_add` 在这里是 `add`）。

**导入名是怎么读出来的**：`LC_SYMTAB` 的未定义符号表是 `nm` 的视图，不是 loader 的——dyld 实际按 `LC_DYLD_INFO` 的 bind 流或 `LC_DYLD_CHAINED_FIXUPS` 的导入表绑定，那里的符号串还可能是 zlib 压缩的。两张表通常一致，但符号表缺失或只写了一半时，只读它会把“不缺”当结论，调用方对着 `not found` 以为是自己拼错了符号名。所以两处都读，和 golem 自己解析的来源对齐。`system_libs` 则是 `LC_LOAD_DYLIB` 的字面声明，所以它总该是 `/usr/lib/libSystem.B.dylib` 这样的真实路径，而不是从符号名反推的猜测。

**读不出来要说读不出来**：导入表解析失败时，`ios_open` / `ios_symbols` 会带上 `imports_error`（依赖库同理是 `system_libs_error`），而不是把它当成「这个 dylib 没有导入」——空的 `imports: []` 和读失败的 `imports: []` 在返回里必须是两回事。

**边界，写在这里而不是藏在错误里**：只有 dylib 自己定义的符号可调用。它从 libSystem / Foundation / Objective-C 导入的东西（`malloc`、`memcpy`、`objc_msgSend` …）既没有 guest 实现，也注册不了宿主桩——golem 的 Darwin `ReplaceFns` 还在 `internal` 里——所以要分两种情形：

- 直接 `ios_call` 这些名字：报 `imported by the dylib but has no implementation`，不返回数字；
- dylib 内部调它：golem 绑的是乐观桩，拿到 0（例如包了 `strlen` 的函数返回 0 而不是长度），不报错。

第二种是设计上的取舍而不是 bug，想提前知道缺什么就看 `ios_imports`。这条边界是第二阶段的入口（`ios_hook`），第一阶段不做。

**加载限制**：golem 不模拟 dyld 的 lazy stub binder，带 lazy/weak bind 流的 dylib 会被拒；`clang -Wl,-bind_at_load` 能绕开，runvm 会把这个建议直接接在错误信息后面。

## H5

| 工具 | 关键参数 | 作用 |
|---|---|---|
| `h5_open` | `target`、`env`、`ephemeral`、`visible`、`webbrain`、`width`/`height` | 打开页面，返回会话 |
| `h5_eval` | `session`、`js`、`timeout_ms` | 执行 JS，Promise 会被 await |
| `h5_wait_for` | `session`、`expression` 或 `selector`、`timeout_ms` | 轮询到条件成立 |
| `h5_console` | `session`、`clear` | 取回 console 与页面异常 |
| `h5_screenshot` | `session`、`path` | 截图（PNG），可同时落盘 |
| `h5_pdf` | `session`、`path` | 导出单页 PDF：macOS 矢量打印，其余平台栅格；结果里 `vector` 说明精度 |
| `h5_close` | `session` | 关掉页面宿主进程 |
| `webbrain` | `session`、`action`、`ref_id`、`text`、… | 用 WebBrain 的页面控制脚本驱动页面（见下） |

`h5_open` 的 `env` 会以 `webframe.env` 暴露给页面；`ephemeral` 默认 `true`，cookie/存储/缓存只留内存。

`h5_eval` 返回的是格式化文本：字符串带引号，对象是 JSON。要结构化数据就在 JS 里 `JSON.stringify`。

### webbrain 工具（WebBrain 页面控制能力）

`h5_open` 传 `"webbrain": true` 时，会在页面加载后自动注入 WebBrain 浏览器扩展的两份内容脚本
（`assets/h5/webbrain/`，从 [webbrain-one/webbrain](https://github.com/webbrain-one/webbrain)
v49.1.2 原样提取：`accessibility-tree.js` + `content.js`，GPL-3.0-or-later；`shim.js` 是 runvm 自己的
MIT 驱动，负责把扩展消息路由桩掉并暴露 `window.__webbrainRun`）。注入后 `webbrain` 工具用同一套
感知/控制栈驱动页面——与 WebBrain 扩展 agent 在浏览器里用的是同一份代码：

| action | 作用 | 关键参数 |
|---|---|---|
| `page` | 页面信息：标题、描述、表单、链接 | — |
| `tree` | 扁平可访问性树，节点带稳定 `ref_id` | `filter`（all/visible/interactive）、`max_depth`、`max_chars`、`ref_id` |
| `interactive` | 可交互元素清单（role/name/ref） | — |
| `snapshot` | `page` + 紧凑 `tree` 一次调用，模型首看常用 | `max_chars` |
| `click` | 点击：优先 `ref_id`（`click_ax`），或 `text`/`selector`/`x`+`y` | `ref_id`、`text`、`text_match`、`selector`、`x`、`y` |
| `type` | 输入文本，可先清空 | `ref_id`、`text`、`clear` |
| `press` | 按键：Escape/Tab/Enter/方向键/分号 | `key`、`repeat` |
| `scroll` | 滚动：`ref_id` 定位或视口 `x`/`y` | `ref_id`、`x`、`y` |
| `check` | 勾选/取消复选框 | `ref_id`、`checked` |

`ref_id` 来自 `tree` / `interactive` / `snapshot`，在元素存活期间稳定（WeakRef 注册表），是
点击/输入/滚动/勾选的首选寻址方式。页面整页导航后注入的全局会被清掉，`webbrain` 工具会在下一次
调用前自动重新注入（`EnsureWebbrain` 先探测 `__webbrainReady`，缺失才重注入）。

## 资源

| URI | MIME | 说明 |
|---|---|---|
| `runvm://status` | application/json | 后端、限制、会话 |
| `runvm://tools` | application/json | 工具目录与 schema |
| `runvm://files/<相对路径>` | 按内容探测 | 可写资源：文本走 `text`，二进制走 `blob`；上传/写入后才出现 |
| `runvm://screenshot/<session>` | image/png | 最近一次截图，截过才存在 |
| `runvm://pdf/<session>` | application/pdf | 最近一次 PDF，导出过才存在 |

`runvm://files/` 是给 HTTP 控制台（阶段 2）定下的契约：`resources/list` 会列出磁盘上真实存在的可写资源（带大小与修改时间），`resources/read` 每次从磁盘读，所以改写后立即反映新内容。

## 提示词

| 名称 | 参数 | 内容 |
|---|---|---|
| `smoke-test-page` | `url`、`expectation` | 打开 → wait_for → 断言 → 截图 → 收 console → 关闭 |
| `inspect-native-lib` | `so`、`symbol` | 加载 .so → 列导出 → 调用 → 必要时 hook → 关闭 |
| `webbrain-automate` | `task`、`url`、`max_steps` | 多步浏览器自动化：打开 → 感知(snapshot/tree) → 按 ref_id 逐步 act → 验证 → 报告 → 关闭 |

提示词参数都是字符串（MCP 协议约定）：`max_steps` 传字符串（如 `"8"`，默认 `"6"`），传数字会报解析错误。

编排：`webbrain-automate` 把上面的感知-计划-行动-验证循环实例化成一段可直接执行的工作流指令。配套的代理技能在 `skills/webbrain/SKILL.md`（可导入任意 MCP 客户端，指挥 LLM 调用 `webbrain` / `h5_*` 工具做多步页面自动化，替代人工点击）。

## 进度与取消

请求带 `_meta.progressToken` 就会收到 `notifications/progress`。stdio 直接交织在输出里；HTTP 会把该次 POST 升级成 SSE 流。不带 token 完全静默。

`h5_open` 报四个阶段（定位二进制 → 启动宿主 → 等待页面 → ready）；`android_open` 与 `ios_open` 报"引擎启动"和"就绪"。

`notifications/cancelled` 会真的中断在跑的调用：`android_call` 在 `pool.Do` 里停下，`h5_eval` 停止等待。

## HTTP 传输

| 路径 | 行为 |
|---|---|
| `POST /mcp` | 单条或批量 JSON-RPC；纯通知返回 `202` 空体 |
| `GET /mcp` | `405` —— 本服务不推送服务端发起的消息 |
| `DELETE /mcp` | `200`（会话由 close 工具关） |
| `GET /healthz` | `ok` |

`initialize` 返回 `Mcp-Session-Id`，本服务不强制回传。
