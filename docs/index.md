---
title: 介绍
layout: page
---

# runvm

**把「虚拟测试环境」做成模型能直接调用的工具。**

模型读代码能猜出一段 native 逻辑大概做什么，但猜不出它返回什么。runvm 给它两个一次性环境：一个能加载并调用 Android `.so` 的模拟器，一个真的 WebKit 页面。两者都通过 [MCP](https://modelcontextprotocol.io) 暴露，模型像调用普通工具一样使用它们。

```
模型 → runvm（MCP 服务）→ android: golem 模拟器 → libsign.so（AArch64）
                        → h5:      webframe    → 系统 WebView → 页面
```

## 它解决什么

| 场景 | 没有 runvm | 有 runvm |
|---|---|---|
| 验证 native 签名函数 | 读汇编、猜参数、上真机 | `android_call fib [20]` → `6765` |
| 测前端行为 | 人肉点页面 | `h5_eval` / `h5_wait_for` / `h5_screenshot` |
| 一份代码三平台 | 各装一套浏览器/JVM | 系统 WebView + 纯 Go 模拟器 |

## 两个后端

**Android** —— 用 [golem](https://github.com/isesword/golem) 在本机加载 `.so`，映射真实的 bionic（`libc/libm/libdl`），执行它自己的 AArch64 指令。**不需要 JVM、不需要真机、不需要 adb**。库的 `init_array` 与 `JNI_OnLoad` 都会跑，导出函数可以直接调用。

**H5** —— 用 [webframe](https://github.com/tractordev/webframe) 把页面跑在系统自带的 WebView 里（WebKitGTK / WKWebView / WebView2）。隐藏运行、不节流，可以执行任意 JS、等待异步 UI、截图、导出 PDF。

## 30 秒看一眼

```jsonc
// 加载一个 .so
{"name":"android_open","arguments":{"so":"/path/libsign.so"}}
// → {"session":"android-1","engine":"unicorn","export_count":9}

// 调用它的导出函数
{"name":"android_call","arguments":{"session":"android-1","symbol":"fib","args":[20]}}
// → {"value":6765,"hex":"0x1a6d","i32":6765}

// 页面里跑一段 JS
{"name":"h5_eval","arguments":{"session":"h5-1","js":"document.title"}}
// → {"result":"\"runvm demo\""}
```

## 平台支持

| | Linux | Windows | macOS |
|---|---|---|---|
| Android 后端（bionic + unicorn） | ✅ | ✅ | ✅ |
| H5 页面（系统 WebView） | ✅ WebKitGTK | ✅ WebView2 | ✅ WKWebView |
| 截图 | ✅ Xvfb + xwd | ✅ PrintWindow | ✅ WKWebView snapshot |
| PDF（栅格） | ✅ | ✅ | ✅ |

三平台都在 CI 里真跑：Linux 跑完整 e2e（39 项断言），macOS/Windows 跑 `doctor` + `selftest`，后者会真的开页面、截图、**统计颜色数**确认不是白屏。

## 接下来

- [快速开始](getting-started.md) —— 三种安装方式、接到 MCP 客户端
- [工具参考](tools.md) —— 15 个工具、4 个资源、2 个提示词
- [技术架构](architecture.md) —— 进程模型、两个 fork、失败语义
- [运行与运维](operations.md) —— 环境变量、会话治理、远程访问
- [现状与缺口](roadmap.md) —— 还有什么没做，为什么
