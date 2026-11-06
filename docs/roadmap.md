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
| PDF（栅格） | ✅ | ✅ | ✅ |
| CI 验证 | 完整 e2e（39 项） | doctor + selftest | doctor + selftest |

## 还没做

**矢量 PDF**。现在的 PDF 是"截图 + 嵌入"，三平台都能出，但是栅格。矢量要三平台的打印 API（`WKWebView.printOperation`、`ICoreWebView2_16.PrintToPdfAsync`、`webkit_print_operation`），和 macOS 截图一样需要继续扩 fork。

**linux-musl / Alpine**。purego 在无 cgo 时只支持 glibc，Alpine 需要 `CGO_ENABLED=1` + `zig cc` 的独立工具链，所以发布矩阵只有 glibc linux / darwin / windows。

**池的重建计数**。`PoolStats` 只有 Size/InUse/Idle，`engines.created` 是自己加的；`MaxUses` 回收次数还没有专门暴露。

**HTTP 的 SSE 重放**。没有实现事件 id + `Last-Event-ID`——本服务不主动推消息，暂时不需要。

## 刻意没做

**不做安全沙箱**。加载的 `.so` 在宿主进程里被模拟执行，页面在真实 WebView 里跑。要隔离就上容器和独立用户，而不是假装这层是边界。

**不做"另开浏览器截图"**。抓到的是重放而不是当前会话状态，对"测完再截图"是假的。

**不做通用 RPC**。MCP 是唯一入口，避免第二套语义漂移。

## 调研存档

- [Windows 截图：为什么按自己的 pid 找不到窗口](research/windows-capture.md)
- [macOS snapshot：block ABI 的可行性评估](research/macos-snapshot.md)
