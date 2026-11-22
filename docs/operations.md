---
title: 运行与运维
layout: page
---

# 运行与运维

## 子命令

| 命令 | 作用 |
|---|---|
| `runvm` / `runvm mcp` | MCP 服务，stdio |
| `runvm http` | 同一套工具，Streamable HTTP |
| `runvm doctor` | 真启动各后端自检 |
| `runvm selftest` | 开页面 → 截图 → 数颜色，验证截图链路 |
| `runvm version` | 版本 |
| `runvm web-host` | 内部：页面宿主子进程 |

## 环境变量

| 变量 | 默认 | 作用 |
|---|---|---|
| `RUNVM_ASSETS` | golem 模块内路径 | bionic sysroot（`assets/android/sdk23`） |
| `RUNVM_ENGINE` | golem 默认 | `android_open` 未指定引擎时的后端 |
| `GOLEM_UNICORN` | 系统搜索 | 显式指定 `libunicorn` 路径 |
| `RUNVM_MAX_SESSIONS` | 8 | 会话总数上限，0 表示不限 |
| `RUNVM_IDLE_TIMEOUT` | `30m` | 空闲多久自动关会话，如 `30m`；`0` 表示关闭 |
| `RUNVM_HTTP_ADDR` | `127.0.0.1:8765` | `runvm http` 默认监听地址 |
| `RUNVM_TOKEN` | 空 | HTTP 的 Bearer token |
| `RUNVM_CONSOLE_USER` | `admin` | 控制台登录用户名 |
| `RUNVM_CONSOLE_PASSWORD` | 空 | 控制台登录密码；未设置时启动生成随机密码打到 stderr，无内置默认密码 |
| `RUNVM_RESOURCES_DIR` | 工作目录下 `runvm-resources` | 可写资源根目录（0700，不存在自动建） |
| `RUNVM_RESOURCES_MAX_SIZE` | `64MiB` | 单个上传/写入上限 |

## 会话治理

长时间跑的服务最怕"模型忘了关"。默认就开空闲回收，加上死会话自动出栈和批量释放：

```bash
RUNVM_MAX_SESSIONS=8 RUNVM_IDLE_TIMEOUT=30m runvm
```

- `RUNVM_IDLE_TIMEOUT` 默认 `30m`（`0` 表示关闭）：后台按每个会话的 `last_used` 回收；**创建即算使用**——第一版忘了这点，刚开的会话被当成"1970 年以来一直空闲"直接收掉。
- **所有**使用会话的入口都会更新 `last_used`（`h5_console` / `h5_screenshot` / `h5_pdf` / `webbrain`，以及 android / ios 的读路径），所以正在用截图、PDF、无障碍树的会话不会被当成空闲收掉。
- **h5 页面宿主进程死了会自己出栈**：页面 `exit()`、崩溃或被杀时，会话自动从注册表移除并释放配额，不需要手动 `h5_close`。android / ios 是进程内模拟器，没有独立的宿主进程，不适用"死会话出栈"——它们的回收只有空闲回收和进程退出时的 `CloseAll`。
- **批量释放**：`runvm_close_all` 一次关掉指定后端（`"backends": ["android", "ios", "h5"]`）或全部（不传 `backends`）。撞到 `RUNVM_MAX_SESSIONS` 时报错会直接列出当前会话和各自的目标，点名该关哪个。
- 被自动回收的会话会留在 `runvm_status` 的 `reclaimed` 历史里（哪几个、为什么、什么时候），模型不会再看到一句没头没尾的 `unknown session`。

> 注意 `last_used` 只在调用时更新。如果你打算开着一个会话十分钟不做任何调用再截图，把 `RUNVM_IDLE_TIMEOUT` 设大或关掉。

## 远程访问的安全边界

```bash
RUNVM_TOKEN=$(openssl rand -hex 16) runvm http -addr 0.0.0.0:8765
```

- **非回环地址 + 没有 token = 拒绝启动**。一个无鉴权的端口等于让网络里的人在你的 native 模拟器里跑代码。
- token 用常量时间比较；401 会带 `WWW-Authenticate`。
- 容器里运行时，宿主机只暴露回环：`-p 127.0.0.1:8765:8765`。

## 控制台

`runvm http` 在 `/` 下提供 SPA 控制台，与 `/mcp` 共用同一个监听端口：

```bash
# 推荐显式设密码；不设则启动生成随机密码并打到 stderr
RUNVM_CONSOLE_USER=admin RUNVM_CONSOLE_PASSWORD='…' runvm http
# runvm: console on http://127.0.0.1:8765/ (login: admin)
```

- **登录**：浏览器打开 `/`，用 `RUNVM_CONSOLE_USER` / `RUNVM_CONSOLE_PASSWORD` 登录；成功下发 `HttpOnly` + `SameSite` 会话 cookie，登出使其失效。未登录访问任意路径（含深链）都渲染登录页，登录后回原路径（`?next=`）。
- **改密码**：设 `RUNVM_CONSOLE_PASSWORD` 环境变量后重启即可；进程内不存任何可写的凭据文件。
- **数据鉴权**：控制台的任何数据接口未登录一律 `401`；控制台登录与 MCP 的 Bearer token 相互独立，控制台不是绕过 MCP 鉴权的入口。
- **构建**：前端是 Vite 产物，提交进 `internal/console/static/dist/` 并由 `go:embed` 内嵌；`make build` / Dockerfile 不依赖 Node，改了 `web/` 源码用 `make web` 重新构建并提交产物（CI 会做漂移检查）。

三段顶层导航，资源管理下四个二级页：

| 页 | 路径 | 用法 |
|---|---|---|
| 系统总览 | `/` | 后端可用性、限额、活跃会话，每 3 秒自动刷新 |
| 资源管理-检索 | `/resources` | 跨类型关键词检索（工具/截图/文件），结果分组，点击跳到子页 |
| 资源管理-工具 | `/resources/tools` | 24 个工具 + input schema，可检索、看参数表与 schema |
| 资源管理-截图 | `/resources/screenshots` | `runvm://screenshot/<session>` 的 PNG，列表 + 预览 + 下载 |
| 资源管理-文件 | `/resources/files` | `runvm://files/` 的文件浏览器（目录树 + 列表 + 按格式打开） |
| MCP 调试 | `/debug` | 见下 |

文件浏览器按格式打开：图片（`png/jpg/jpeg/gif/webp/bmp/svg`）预览；`json` 树视图 + 编辑（保存前 `JSON.parse` 校验，非法 JSON 报错并阻止保存）；`md` 预览 + 编辑；`txt` 及文本类（`log/yaml/yml/toml/csv/ini/conf/go/js/css/html/sh`）直接编辑；其它二进制显示大小 / MIME / sha256 / 修改时间并下载。编辑保存走 `PUT /api/resources/file`，保存后 `resources/read` 立即读到新内容；上传并入文件页（拖拽或选择文件），与 `runvm_upload` 同一套校验与上限。只读资源（`runvm://tools`/`runvm://status`/截图/pdf）不给编辑入口。

语言切换在顶栏右侧「中文 / EN」分段控件，切换不刷新整页并记住偏好；文案走 `web/locales/{zh-CN,en-US}.json`，构建期 inline，工具名/参数名/schema/URI/时间戳/后端原始错误不翻译。

### 调试页怎么用

调试页连的是**进程内 runvm 自己**，且**只读**——用来调 runvm 自己的 MCP server 并看协议报文，不是第二个控制台：

- 顶部连接条显示真实连接信息：server 名称/版本、协商后的协议版本、capabilities 列表（`resources.listChanged` 为 true，`tools`/`prompts` 为 false），均来自 `initialize` 的真实返回；
- 列出当前 24 个工具 + 5 种资源形态 + 3 个提示词；资源/提示词显示描述文字，点选即预填 `resources/read` / `prompts/get` 参数；
- `tools/call` 走**白名单**，只有确实只读的工具（`runvm_status` / `android_symbols` / `android_java_stubs` / `ios_symbols` / `ios_imports` / `h5_console`）可调，写工具与所有状态变更工具（含 `runvm_close_all`）**没有调用入口**，只列出供参考；
- 结果按内容分类：图片看图、JSON 树视图、PDF/二进制给大小与下载；每次请求/响应都有 JSON-RPC 原文；
- schema 可移植性检查按 Inspector 的 4 条规则（`boolean-schema`/`type-union`/`untyped-schema`/`remote-ref`）报问题；当前 runvm 基线为 0 error / 3 warning；
- 进程内直调不提供 SSE 进度流（界面明确标注「本连接不提供进度流」），不要依赖进度通知。

## 可观测性

`runvm_status` 是主要入口：

```json
{
  "android": {"sessions": 1, "assets_ok": true},
  "limits": {"max_sessions": 8, "idle_timeout": "30m0s"},
  "sessions": {
    "android": [{"session": "android-1", "pool": 2,
                 "engines": {"in_use": 0, "idle": 2, "created": 2}}],
    "ios": ["ios-1"],
    "h5": [{"session": "h5-1", "target": "https://example.com",
            "last_used": "2026-11-22T05:00:00Z", "state": "alive"}]
  },
  "reclaimed": [{"session": "h5-2", "backend": "h5", "reason": "dead", "at": "2026-11-22T04:59:00Z"}]
}
```

`engines.created` 超过池大小，说明引擎在回收或重建——长跑时这是"为什么变慢了"的第一手线索。`reclaimed` 说明哪些会话被服务端自动关掉、为什么、什么时候。

## 容器

```bash
docker run --rm ghcr.io/dolfly/runvm doctor
docker run --rm -p 127.0.0.1:8765:8765 \
  -e RUNVM_TOKEN=$TOKEN ghcr.io/dolfly/runvm http -addr 0.0.0.0:8765
```

镜像非 root（uid 10001），`/work` 可写，`HEALTHCHECK` 用 `runvm version`。多架构：`linux/amd64`、`linux/arm64`。

## 排障

| 症状 | 多半是 |
|---|---|
| `gtk_init_check failed` | 缺 Xvfb 或 DISPLAY；`apt install xvfb` |
| `WebKitGTK not found` | 缺 `libwebkit2gtk-4.1-0` |
| `cannot load libunicorn` | 缺 `libunicorn2`，或 `GOLEM_UNICORN` 指错 |
| `bionic sysroot not found` | `RUNVM_ASSETS` 没指到 golem 的 `assets` |
| 截图是白的 | 没开 `visible: true`；或窗口还没画完（`selftest` 会抓到） |
