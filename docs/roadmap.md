---
title: 现状与缺口
layout: page
---

# 现状与缺口

按"现在能不能做、做了会不会骗人"排序，而不是按听起来是否带劲。

## 已具备

| | Linux | Windows | macOS |
|---|---|---|---|
| Android 后端 | ✅ | ✅ | ✅ |
| H5 页面 | ✅ | ✅ | ✅ |
| 截图 | ✅ | ✅ | ✅ |
| PDF | ✅ 栅格 | ✅ 栅格 | ✅ 矢量 |
| CI 验证 | 完整 e2e（39 项） | doctor + selftest | doctor + selftest |

## 还没做

**矢量 PDF 的另外两个平台**。macOS 已经打通：`WKWebView.createPDFWithConfiguration:` 由 `dolfly/glaze` v0.4.0 的 `PrintPDF()` 承载，`dolfly/webframe-fork` v0.3.0 透出 `Frame.PrintPDF()`，`h5_pdf` 先打印、打印不了才回落到栅格，结果里的 `vector` 字段说明是哪一种。剩下 Windows 的 `ICoreWebView2_16.PrintToPdfAsync`（COM，走 purego）和 Linux 的 `webkit_print_operation`（GTK 打印设置指向文件）。

**linux-musl / Alpine**。purego 在无 cgo 时只支持 glibc，Alpine 需要 `CGO_ENABLED=1` + `zig cc` 的独立工具链，所以发布矩阵只有 glibc linux / darwin / windows。

**池的重建计数**。`PoolStats` 只有 Size/InUse/Idle，`engines.created` 是自己加的；`MaxUses` 回收次数还没有专门暴露。

**iOS 的 `.so`**。golem 已经能加载 Mach-O arm64 并按魔数选择 Darwin 人格：本机实测 `add(3,4)=7`、`getpid()=4242` 都通（[调研](research/ios-so.md)）。缺的是 `libSystem` 那一层——iOS 没有可分发的共享库，碰 `malloc`/`objc_msgSend` 的库要靠宿主侧按符号提供实现，而 Darwin 的 `ReplaceFns` 目前还在 golem 的 `internal` 里，外部模块注册不了。纯计算类（签名、加解密）现在就能跑，第一步是把 runvm 里「按格式分流」做掉。

**HTTP 的 SSE 重放**。没有实现事件 id + `Last-Event-ID`——本服务不主动推消息，暂时不需要。

## 刻意没做

**不做安全沙箱**。加载的 `.so` 在宿主进程里被模拟执行，页面在真实 WebView 里跑。要隔离就上容器和独立用户，而不是假装这层是边界。

**不做"另开浏览器截图"**。抓到的是重放而不是当前会话状态，对"测完再截图"是假的。

**不做通用 RPC**。MCP 是唯一入口，避免第二套语义漂移。

## 调研存档

- [Windows 截图：为什么按自己的 pid 找不到窗口](research/windows-capture.md)
- [macOS snapshot：block ABI 的可行性评估](research/macos-snapshot.md)
- [iOS `.so`：能不能像 Android 那样在本机加载执行](research/ios-so.md)
