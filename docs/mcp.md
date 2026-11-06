# MCP 工具参考

传输:JSON-RPC 2.0,stdio,一行一条消息。协议版本 `2025-06-18`(兼容 `2025-03-26`、`2024-11-05`)。

服务器只声明 `tools` 能力;`resources/list` 与 `prompts/list` 返回空数组以便客户端探测。

## 通用约定

- 工具失败返回 `result.isError = true` 与一段可读的错误文本(不是 JSON-RPC error)。
- 协议层错误才用 JSON-RPC error:未知方法 `-32601`、未知工具 `-32602`、坏 JSON `-32700`。
- 单次 `tools/call` 上限 120 秒。
- 会话 ID 形如 `android-1`、`h5-2`,由服务器按创建顺序分配。

---

## runvm_status

无参数。报告后端可用性与活跃会话,**不启动任何东西**(便宜)。

```json
{
  "version": "dev",
  "go": "go1.27.3",
  "os": "linux",
  "arch": "amd64",
  "android": {"sessions": 0, "assets": "/…/golem/assets", "assets_ok": true},
  "limits": {"max_sessions": 8, "idle_timeout": "0s"},
  "sessions": {"android": [{"session": "android-1", "pool": 2, "engines": {"in_use": 0, "idle": 2}}]},
  "h5": {"sessions": 0},
  "sessions": {"android": [], "h5": []}
}
```

## android_open

加载一个 Android native 库,跑完初始化,返回会话。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `so` | string | ✓ | `.so` 在本机的路径(AArch64 / ARMv7 / x86_64) |
| `engine` | string | | CPU 后端,默认取 `RUNVM_ENGINE`,再默认由 golem 决定 |
| `dex` | string | | `classes.dex`,让 `FindClass/GetMethodID` 能按真实签名解析 |

```json
{"session": "android-1", "engine": "unicorn", "arch": "EM_AARCH64",
 "export_count": 9, "exports_preview": ["add", "fib", "slen"]}
```

失败原因通常是:文件不存在、sysroot 没找到(设 `RUNVM_ASSETS`)、没有可用引擎(装 `libunicorn` 并用 `-tags unicorn` 编译)。

## android_symbols

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `session` | string | ✓ | `android_open` 返回的 id |
| `filter` | string | | 大小写不敏感的子串匹配 |
| `limit` | integer | | 默认 200 |

```json
{"total": 9, "symbols": ["add", "fib", "slen"], "truncated": false}
```

直接读 ELF,所以列导出表本身不需要 emulator 活着。

## android_java_stub

库通过 JNI 向 Java 侧要东西时(取系统时间、包名、设备 ID),真的 Android 运行时不在这里。这个工具让宿主替它回答。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `session` | string | ✓ | |
| `class` | string | ✓ | `java/lang/System`、`java.lang.System`、`Ljava/lang/System;` 都接受 |
| `method` | string | ✓ | 方法名;给完整签名也行,括号前的部分生效 |
| `returns` | string \| number \| boolean \| null | | 返回值,按调用点的类型强制转换 |

```json
{"stub": "java/lang/System->currentTimeMillis", "returns": "1700000000000"}
{"stub": "android.content.Context->getPackageName", "returns": "\"com.example.app\""}
```

一个数值桩同时应答 `int` / `long` / `boolean` 调用;字符串桩应答返回对象的调用;`null` 表示"这个 Java 对象不存在"。没注册的回调落到默认值(`null` / `0`)——和设备上缺类时的行为一致。同一个 class+method 重复注册会被拒绝。

## android_java_stubs

| 参数 | 类型 | 必填 |
|---|---|---|
| `session` | string | ✓ |

```json
{"stubs": [{"class": "android.content.Context", "method": "getPackageName", "kind": "string", "string": "com.example.app"}]}
```

## android_call

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `session` | string | ✓ | |
| `symbol` | string | ✓ | 导出函数名 |
| `args` | array | | ABI 顺序,最多 8 个 |

`args` 的三种元素:

```jsonc
2                       // word:整数参数
"hello"                 // char*:字符串拷进 guest 内存并传指针
{"buffer": 64}          // 出参:分配 64 字节,调用后读回
```

```json
{"symbol": "add", "value": 5, "hex": "0x5", "i32": 5}
{"symbol": "sum_into", "value": 0,
 "buffers": [{"size": 4, "hex": "2a000000", "ascii": "*"}]}
```

负数要按补码传(`-1` 传 `4294967295`),因为 ABI 宽度由目标架构决定,服务器不替你猜。

## android_close

| 参数 | 类型 | 必填 |
|---|---|---|
| `session` | string | ✓ |

```json
{"closed": "android-1"}
```

## h5_open

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `target` | string | ✓ | URL、本地文件路径,或 `about:blank` |
| `env` | object | | 字符串表,页面里以 `webframe.env` 可见(任何页面脚本都能读) |
| `ephemeral` | boolean | | 默认 `true`:cookie/存储/缓存只留内存 |
| `visible` | boolean | | 画进窗口(私有 Xvfb),截图必须开 |
| `width` / `height` | integer | | 窗口尺寸,默认 1280×800 |

```json
{"session": "h5-1", "target": "examples/h5/page.html"}
```

阻塞到页面的 hooks 装好(`ready`)为止,默认 30 秒超时。

## h5_eval

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `session` | string | ✓ | |
| `js` | string | ✓ | 表达式或语句块;Promise 会被 await |
| `timeout_ms` | integer | | 默认 30000 |

```json
{"result": "\"runvm demo\""}
```

结果是 webframe 的格式化文本:字符串带引号,对象是 JSON。想拿结构化数据,在 JS 里 `JSON.stringify(...)` 后自己解析,或者直接断言成布尔值。

## h5_console

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `session` | string | ✓ | |
| `clear` | boolean | | 默认 `true` |

```json
{"count": 2,
 "lines": [{"level": "log", "text": "page ready"},
           {"level": "error", "text": "Uncaught Error: boom"}]}
```

`level` 为 `log`/`warn`/`error`/`assert`/`dialog`(弹窗被拦截时)以及 `load`(页面加载完成)、`error`(页面异常)。

## h5_wait_for

轮询直到条件成立——异步 UI 的正解,不用 sleep 猜时间。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `session` | string | ✓ | |
| `expression` | string | | 反复求值直到为真 |
| `selector` | string | | `expression=document.querySelector(<selector>)` 的简写 |
| `timeout_ms` | integer | | 默认 10000 |

```json
{"matched": true, "value": {}, "waited_ms": 386}
```

表达式在页面里被 `try/catch` 包住并序列化成 JSON,所以中途抛异常会作为"还没成立"继续等,而不是炸掉调用。超时报错会带上最后一次取到的值。

## h5_screenshot

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `session` | string | ✓ | 用 `visible: true` 打开的会话 |
| `path` | string | | 同时把 PNG 写到这个路径 |

返回两段 content:`text`(说明与路径)和 `image`(`image/png`,base64)。

前提有三条,每条有自己的错误信息:会话是可见的、宿主自己拥有 display(所以抓到的是页面不是桌面)、平台是 Linux。截图前会等两帧 `requestAnimationFrame` + 120ms,避免抓到"布局好了但还没画"的白屏。

## h5_close

| 参数 | 类型 | 必填 |
|---|---|---|
| `session` | string | ✓ |

```json
{"closed": "h5-1"}
```

---

## HTTP 传输

stdio 之外,同一套工具可以从 `POST /mcp` 调用:

```bash
curl -s -X POST http://127.0.0.1:8765/mcp \
  -H "Authorization: Bearer $RUNVM_TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

- 批量:发 JSON 数组,收回数组;只有通知时返回 `202`。
- 鉴权:设置 `RUNVM_TOKEN` 或 `-token` 后要求 `Authorization: Bearer <token>`(常量时间比较)。
- `GET /mcp` 返回 `405`:本服务不推送服务端发起的消息。

## 一次完整会话

```
→ {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}
← {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18",
     "capabilities":{"tools":{"listChanged":false}},
     "serverInfo":{"name":"runvm","version":"dev"}}}

→ {"jsonrpc":"2.0","method":"notifications/initialized"}

→ {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"h5_open",
     "arguments":{"target":"examples/h5/page.html"}}}
← {"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text",
     "text":"{\n  \"session\": \"h5-1\",\n  \"target\": \"examples/h5/page.html\"\n}"}]}}

→ {"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"h5_eval",
     "arguments":{"session":"h5-1","js":"document.querySelector('#save').click(); document.querySelector('#app').dataset.saved"}}}
← {"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"{\n  \"result\": \"\\\"yes\\\"\"\n}"}]}}

→ {"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"h5_close",
     "arguments":{"session":"h5-1"}}}
← {"jsonrpc":"2.0","id":4,"result":{"content":[{"type":"text","text":"{\n  \"closed\": \"h5-1\"\n}"}]}}
```

手搓一遍:

```bash
./bin/runvm <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"runvm_status","arguments":{}}}
EOF
```
