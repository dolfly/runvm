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
| iOS dylib（纯计算） | ✅ 需 libunicorn | ✅ 需 libunicorn | ✅ 需 libunicorn |
| H5 页面 | ✅ | ✅ | ✅ |
| 截图 | ✅ | ✅ | ✅ |
| PDF | ✅ 栅格 | ✅ 栅格 | ✅ 矢量 |
| CI 验证 | 完整 e2e（39 项） | doctor + selftest | doctor + selftest |

## 还没做

**矢量 PDF 的另外两个平台**。macOS 已经打通：`WKWebView.createPDFWithConfiguration:` 由 `dolfly/glaze` v0.4.0 的 `PrintPDF()` 承载，`dolfly/webframe-fork` v0.3.0 透出 `Frame.PrintPDF()`，`h5_pdf` 先打印、打印不了才回落到栅格，结果里的 `vector` 字段说明是哪一种。剩下 Windows 的 `ICoreWebView2_16.PrintToPdfAsync`（COM，走 purego）和 Linux 的 `webkit_print_operation`（GTK 打印设置指向文件）。

**linux-musl / Alpine**。purego 在无 cgo 时只支持 glibc，Alpine 需要 `CGO_ENABLED=1` + `zig cc` 的独立工具链，所以发布矩阵只有 glibc linux / darwin / windows。

**池的重建计数**。`PoolStats` 只有 Size/InUse/Idle，`engines.created` 是自己加的；`MaxUses` 回收次数还没有专门暴露。

**iOS 的 libSystem 那一层**。第一阶段（Mach-O 分流 + `ios_open`/`ios_call`）已落地，见下。剩下的是 iOS 没有可分发的共享库这件事：碰 `malloc`/`objc_msgSend` 的库要靠宿主侧按符号提供实现，而 Darwin 的 `ReplaceFns` 目前还在 golem 的 `internal` 里，外部模块注册不了。

**HTTP 的 SSE 重放**。没有实现事件 id + `Last-Event-ID`——本服务不主动推消息，暂时不需要。

## iOS 后端（第一阶段已做）

**分流点是容器格式，不是扩展名**。golem 自己按魔数选人格（`loader.Sniff`），runvm 在 `android.Open` 之前判一次 Mach-O：是就不喂 bionic sysroot、不建 JNI 表，直接交给 Darwin 人格。`android_open` 遇到 Mach-O 也会转到这条路上，返回 `ios-N` 会话（结果里带 `routed_to: "ios"`），调用方不必换工具。

已具备：

| 能力 | 落地 |
|---|---|
| Mach-O arm64 加载 + init | `ios_open`（无 `assets:` 段、无 auxv，Darwin 默认人格） |
| 导出符号发现 | `ios_symbols`（读 Mach-O 符号表，不用引擎、不用会话） |
| 导入符号与依赖库 | `ios_imports`（同时读 `LC_SYMTAB` 与 bind 流 / chained fixups）与 `ios_open` 结果里的 `system_libs`（`LC_LOAD_DYLIB` 声明） |
| 调导出函数 | `ios_call`（整数 / `char*` / 出参缓冲，最多 8 个，符号名不带前导下划线） |
| 引擎池与会话治理 | `pool` / `max_uses`，与 android、h5 共用 `RUNVM_MAX_SESSIONS` |
| doctor / selftest 自检 | 仓库内嵌一个 arm64 dylib 探针，`add(3,4)` 期望 7——是真跑，不是解析 |

**加载限制**：golem 不模拟 dyld 的 lazy stub binder，带 lazy/weak bind 流的 dylib 会被直接拒绝（诊断很明确）。`-Wl,-bind_at_load` 把 bind 放到非 lazy 流里就能跑；runvm 把这个建议拼在错误信息后面，免得调用方对着 golem 的原始报错猜。

**没做，而且这一层做不到的**：libSystem / Foundation / Objective-C。dylib 从系统库导入的符号既没有 guest 实现，也注册不了宿主桩。分两种情形，必须分清：

- **直接调它**：`ios_call` 报「imported by the dylib but has no implementation」，不返回数字；
- **dylib 内部调它**：golem 把未解析导入绑到乐观桩，拿到 0（例如自己包了 `strlen` 的函数返回 0 而不是长度）。这不是错误，是设计上的取舍，所以要靠 `ios_imports` 先看清缺什么。

这道门是第二阶段 `ios_hook` 的前提：等 golem 把 Darwin 的 `ReplaceFns` 开出门面。

## 刻意没做

**不做安全沙箱**。加载的 `.so` 在宿主进程里被模拟执行，页面在真实 WebView 里跑。要隔离就上容器和独立用户，而不是假装这层是边界。

**不做"另开浏览器截图"**。抓到的是重放而不是当前会话状态，对"测完再截图"是假的。

**不做通用 RPC**。MCP 是唯一入口，避免第二套语义漂移。

## 调研存档

- [Windows 截图：为什么按自己的 pid 找不到窗口](research/windows-capture.md)
- [macOS snapshot：block ABI 的可行性评估](research/macos-snapshot.md)
- [iOS `.so`：能不能像 Android 那样在本机加载执行](research/ios-so.md)
