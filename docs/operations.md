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
| `runvm doctor` | 真启动两个后端自检 |
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
| `RUNVM_IDLE_TIMEOUT` | 关 | 空闲多久自动关会话，如 `30m` |
| `RUNVM_HTTP_ADDR` | `127.0.0.1:8765` | `runvm http` 默认监听地址 |
| `RUNVM_TOKEN` | 空 | HTTP 的 Bearer token |

## 会话治理

长时间跑的服务最怕"模型忘了关"。两个开关一起用：

```bash
RUNVM_MAX_SESSIONS=8 RUNVM_IDLE_TIMEOUT=30m runvm
```

- 超过上限时，开新会话会报错并点名用哪个工具腾位置；
- 后台按每个会话的 `last_used` 回收；**创建即算使用**——第一版忘了这点，刚开的会话被当成"1970 年以来一直空闲"直接收掉。

> 注意 `last_used` 只在调用时更新。如果你打算开着一个会话十分钟不动再截图，把 `RUNVM_IDLE_TIMEOUT` 设大或关掉。

## 远程访问的安全边界

```bash
RUNVM_TOKEN=$(openssl rand -hex 16) runvm http -addr 0.0.0.0:8765
```

- **非回环地址 + 没有 token = 拒绝启动**。一个无鉴权的端口等于让网络里的人在你的 native 模拟器里跑代码。
- token 用常量时间比较；401 会带 `WWW-Authenticate`。
- 容器里运行时，宿主机只暴露回环：`-p 127.0.0.1:8765:8765`。

## 可观测性

`runvm_status` 是主要入口：

```json
{
  "android": {"sessions": 1, "assets_ok": true},
  "limits": {"max_sessions": 8, "idle_timeout": "0s"},
  "sessions": {"android": [{"session": "android-1", "pool": 2,
                            "engines": {"in_use": 0, "idle": 2, "created": 2}}]}
}
```

`engines.created` 超过池大小，说明引擎在回收或重建——长跑时这是"为什么变慢了"的第一手线索。

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
