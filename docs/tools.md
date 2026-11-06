---
title: 工具参考
layout: page
---

# 工具参考

传输是 JSON-RPC 2.0：stdio 一行一条消息；HTTP 走 `POST /mcp`。协议版本 `2025-06-18`（兼容 `2025-03-26`、`2024-11-05`）。

约定：工具执行失败返回 `result.isError = true` 与一段可读文本（模型能读、能自己修）；协议层错误才用 JSON-RPC error。单次调用上限 120 秒。会话 ID 形如 `android-1`、`h5-2`。

## 通用

| 工具 | 作用 |
|---|---|
| `runvm_status` | 后端可用性、限制、活跃会话。不启动任何东西 |

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

## H5

| 工具 | 关键参数 | 作用 |
|---|---|---|
| `h5_open` | `target`、`env`、`ephemeral`、`visible`、`width`/`height` | 打开页面，返回会话 |
| `h5_eval` | `session`、`js`、`timeout_ms` | 执行 JS，Promise 会被 await |
| `h5_wait_for` | `session`、`expression` 或 `selector`、`timeout_ms` | 轮询到条件成立 |
| `h5_console` | `session`、`clear` | 取回 console 与页面异常 |
| `h5_screenshot` | `session`、`path` | 截图（PNG），可同时落盘 |
| `h5_pdf` | `session`、`path` | 导出单页 PDF（栅格） |
| `h5_close` | `session` | 关掉页面宿主进程 |

`h5_open` 的 `env` 会以 `webframe.env` 暴露给页面；`ephemeral` 默认 `true`，cookie/存储/缓存只留内存。

`h5_eval` 返回的是格式化文本：字符串带引号，对象是 JSON。要结构化数据就在 JS 里 `JSON.stringify`。

## 资源

| URI | MIME | 说明 |
|---|---|---|
| `runvm://status` | application/json | 后端、限制、会话 |
| `runvm://tools` | application/json | 工具目录与 schema |
| `runvm://screenshot/<session>` | image/png | 最近一次截图，截过才存在 |
| `runvm://pdf/<session>` | application/pdf | 最近一次 PDF，导出过才存在 |

## 提示词

| 名称 | 参数 | 内容 |
|---|---|---|
| `smoke-test-page` | `url`、`expectation` | 打开 → wait_for → 断言 → 截图 → 收 console → 关闭 |
| `inspect-native-lib` | `so`、`symbol` | 加载 .so → 列导出 → 调用 → 必要时 hook → 关闭 |

## 进度与取消

请求带 `_meta.progressToken` 就会收到 `notifications/progress`。stdio 直接交织在输出里；HTTP 会把该次 POST 升级成 SSE 流。不带 token 完全静默。

`h5_open` 报四个阶段（定位二进制 → 启动宿主 → 等待页面 → ready）；`android_open` 报"读库"和"引擎就绪"。

`notifications/cancelled` 会真的中断在跑的调用：`android_call` 在 `pool.Do` 里停下，`h5_eval` 停止等待。

## HTTP 传输

| 路径 | 行为 |
|---|---|
| `POST /mcp` | 单条或批量 JSON-RPC；纯通知返回 `202` 空体 |
| `GET /mcp` | `405` —— 本服务不推送服务端发起的消息 |
| `DELETE /mcp` | `200`（会话由 close 工具关） |
| `GET /healthz` | `ok` |

`initialize` 返回 `Mcp-Session-Id`，本服务不强制回传。
