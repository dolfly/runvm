---
title: 快速开始
layout: page
---

# 快速开始

## 安装

**下载二进制**（推荐）：从 [Releases](https://github.com/dolfly/runvm/releases) 取对应平台的 zip，解压即用，无 cgo 依赖。

```bash
unzip runvm_0.9.0_linux_amd64.zip
./runvm version
```

**Docker**：镜像里已经把两个后端要的运行时装齐了（WebKitGTK、Xvfb、xwd、Mesa、libunicorn、中文字体）。

```bash
docker run --rm ghcr.io/dolfly/runvm doctor      # 自检
docker run --rm -i ghcr.io/dolfly/runvm          # MCP over stdio
```

**从源码**：Go 1.27+；Linux 需要 `libwebkit2gtk-4.1-0`、`xvfb`、`libgl1-mesa-dri`、`x11-apps`；Android 后端需要 `libunicorn2`。

```bash
git clone https://github.com/dolfly/runvm && cd runvm
make build        # -tags unicorn，把 CPU 引擎编进来
./bin/runvm doctor
```

## 先自检

```bash
$ runvm doctor
runvm 0.9.0 — go go1.27.3, linux/amd64 — ok

 ✓ bionic sysroot   /opt/runvm/assets
 ✓ cpu engine       booted bionic in 14ms
 ✓ webview          about:blank ready in 858ms
```

`doctor` 不是查版本号，它会**真的启动**一个 emulator 和一个 WebView。任何一项失败退出码非 0。

截图另有 `runvm selftest`：开一个两色页面、截图、统计颜色数，白屏会被判失败。

## 接到 MCP 客户端

**stdio**（客户端负责拉起进程）：

```json
{"mcpServers":{"runvm":{
  "command":"/abs/path/bin/runvm",
  "args":[],
  "env":{"RUNVM_ASSETS":"/path/to/golem/assets"}}}}
```

**HTTP**（客户端拿不到进程时）：

```bash
RUNVM_TOKEN=$(openssl rand -hex 16) runvm http -addr 0.0.0.0:8765
```

```json
{"mcpServers":{"runvm":{
  "type":"http","url":"https://your-host/mcp",
  "headers":{"Authorization":"Bearer ${RUNVM_TOKEN}"}}}}
```

## 第一个会话

**跑一段 native 代码**

```jsonc
{"name":"android_open","arguments":{"so":"/path/libfoo.so"}}
{"name":"android_symbols","arguments":{"session":"android-1","filter":"sign"}}
{"name":"android_call","arguments":{"session":"android-1","symbol":"sign","args":["hello"]}}
{"name":"android_close","arguments":{"session":"android-1"}}
```

`android_call` 的参数有三种形态，一条调用覆盖大多数 native 接口：

```jsonc
[2, 3]                        // 整数
["hello"]                     // char*，自动拷进 guest 内存
[{"buffer": 64}, 20]          // 出参：64 字节缓冲区，调用后读回 hex/ascii
```

**测一个页面**

```jsonc
{"name":"h5_open","arguments":{"target":"https://example.com","visible":true}}
{"name":"h5_wait_for","arguments":{"session":"h5-1","selector":"h1","timeout_ms":5000}}
{"name":"h5_eval","arguments":{"session":"h5-1","js":"document.title"}}
{"name":"h5_console","arguments":{"session":"h5-1"}}
{"name":"h5_screenshot","arguments":{"session":"h5-1","path":"shot.png"}}
{"name":"h5_close","arguments":{"session":"h5-1"}}
```

`visible: true` 只在需要截图/PDF 时才要——它让页面画进一个窗口（Linux 是私有 Xvfb，macOS 直接栅格化，不需要窗口）。
