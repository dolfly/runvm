---
title: 技术架构
layout: page
---

# 技术架构

## 三层 + 一条桥

```
模型 / MCP 客户端
   │ JSON-RPC 2.0（stdio 或 HTTP）
   ▼
runvm（MCP 服务，进程常驻）
   ├── internal/mcp       传输、进度、取消、资源、提示词（零依赖）
   ├── internal/tools     15 个工具的参数校验与编排
   ├── internal/session   会话注册表（泛型 + last_used）
   ├── internal/android   golem 会话池 + ELF 导出表 + JNI 桩
   └── internal/h5        webframe 宿主（子进程）+ 平台截图
```

## 为什么 H5 要出进程

webframe 的 `Frame` 必须建在主 goroutine 上，`Run()` 会一直占着它——**一个页面锁死一个进程**。而 MCP 服务要长期存活并同时服务多个会话，两者无法共处一个主线程。

所以每个页面是一个 `runvm web-host` 子进程，父子之间走一行一个 JSON 的管道：

```
runvm（主进程，MCP 读循环 + 每请求一个 goroutine）
  │ JSONL：eval / snapshot / close
  ▼
runvm web-host（子进程，主 goroutine = WebView 事件循环）
  └── Xvfb（Linux，无 DISPLAY 时；Pdeathsig 绑在宿主主线程）
```

好处：页面崩了不带倒服务器；坏处：一个页面 = 一个进程 + 一次冷启动（0.5–1.5 秒）。

## 并发模型

- **请求**并发处理（上限 32 在飞），响应可能乱序——JSON-RPC 允许，客户端按 `id` 对。
- **同一页面**的 `h5_eval` 串行：页面是单线程的，交错会让副作用顺序不可预测。
- **Android** 用 golem 的引擎池：引擎借给一个调用者独占，并发靠引擎数（`"pool": N`），`.so` 只读页零拷贝共享。

## 两条数据路径

**页面 → 模型**：`console.log` / 异常 / `webframe.exit` → 注入的 hooks → Go 回调 → JSONL 一行 → 父进程环形缓冲（上限 500 行）→ `h5_console` 取走。

**模型 → 页面**：`h5_eval` 分配 id、挂 pending、写一行 → 子进程 `Frame.Eval` → 回调带回结果 → 父进程按 id 唤醒等待者。三条退出路径都覆盖：结果、页面关闭、超时。

## 两个 fork

| fork | 加了什么 | 为什么必须 fork |
|---|---|---|
| `dolfly/glaze` | `Snapshotter` 旁路接口 + macOS `takeSnapshotWithConfiguration:` | 上游没有截图 API |
| `dolfly/webframe-fork` | `Frame.Snapshot()` | `Frame` 把 `glaze.WebView` 藏着 |

`Snapshotter` **不是** `WebView` 的方法——加在接口上会让 Linux/Windows 后端编译不过。两者都按 tag 钉住，runvm 只保留一条 `replace`。

## 截图的三条路

| 平台 | 做法 | 前提 |
|---|---|---|
| Linux | 私有 Xvfb（屏幕=视口）+ `xwd` 抓根窗口 + Go 解 XWD | 页面 `visible: true` |
| Windows | `PrintWindow` + `PW_RENDERFULLCONTENT` 抓**宿主子进程**的窗口 | 同上 |
| macOS | WKWebView 自己栅格化 → NSImage → TIFF → PNG | 无 |

Windows 那条踩过一个坑：窗口在子进程里，而抓取在父进程里按自己的 pid 枚举，永远是 0。把宿主 pid 传下去就解决了。

## 失败语义

不做"假装健康"：工具失败是 `isError` + 人话；页面加载失败立刻返回并带上页面自己的异常；`h5_open` 30 秒没 ready 就杀宿主再报错；会话 ID 不存在会告诉你用哪个工具开。

## 它不是什么

**不是安全沙箱**。加载的 `.so` 在你进程里被模拟执行，页面在真实 WebView 里跑。只跑可信代码。`RUNVM_TOKEN` 保护的是 MCP 端口，不是页面。

更多细节：[Windows 抓取的诊断过程](research/windows-capture.md)、[macOS snapshot 的可行性评估](research/macos-snapshot.md)。
