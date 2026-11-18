# WebBrain × runvm — 把开源 AI 浏览器代理跑起来做真实自动化

本示例演示如何把 [WebBrain](https://github.com/webbrain-one/webbrain)（开源 AI 浏览器代理扩展）作为真实自动化目标：

1. **编译扩展**：产出 Chrome / Firefox 双端构建与商店 zip。
2. **跑真实自动化**：用 runvm 的 H5 后端直接驱动目标网页（h5_open / h5_eval / h5_screenshot），再走**真实扩展路径**——Chromium 加载扩展 + webbrain MCP server 委托 AI 代理在页面上执行 Ask（只读分析）与 Act（滚动/点击）任务。

## 为什么有两条路径

| 路径 | 引擎 | 能加载 MV3 Chrome 扩展？ | 典型用途 |
|---|---|---|---|
| runvm H5 后端 | 系统 WebView（WebKitGTK / WKWebView / WebView2） | **不能**（Chrome 扩展只跑在 Chromium） | 纯页面打开、JS 注入、DOM 断言、截图、PDF |
| Playwright + Chromium `--load-extension` | Chromium | **能** | 加载 webbrain 扩展本体，走它的 AI agent 循环 |

runvm 的 H5 后端是 WebKit 视图，无法加载 Manifest V3 扩展；要跑 webbrain 扩展本体必须用 Chromium。两者互补：runvm 提供免安装的页面自动化，Chromium + 扩展提供完整的 AI 浏览器代理。

## 快速开始

### 0. 前置

- Node 20+（编译 webbrain 与跑驱动脚本）
- Go 1.27+ / 已发布的 runvm 二进制（H5 后端）
- 一个 OpenAI 兼容或 Anthropic 兼容的 LLM 端点（webbrain 的 agent 循环需要模型；本示例默认火山方舟 Coding Plan 的 Anthropic 兼容端点）

### 1. 编译 webbrain 扩展

```bash
git clone https://github.com/webbrain-one/webbrain.git && cd webbrain
npm ci && npm install-scripts approve esbuild   # npm 11 需放行 esbuild 的 postinstall
npm run build:chrome   # → build/chrome（Chromium 加载目录）
npm run build:firefox  # → build/firefox
npm run build:zip      # → dist/webbrain-chrome-<ver>.zip 等商店包
```

### 2. runvm H5 路径：直接驱动小红书

```bash
runvm http -addr 127.0.0.1:41000   # 或 multica env start
# MCP 调用：
#   h5_open    {"target":"https://www.xiaohongshu.com","visible":false,"width":1280,"height":720}
#   h5_eval    {"session":"h5-1","js":"document.title"}
#   h5_screenshot {"session":"h5-1","path":"xhs.png"}
#   h5_close   {"session":"h5-1"}
```

实测（macOS）：标题 `小红书 - 你的生活兴趣社区`、跳转 `/explore`、DOM 可读（150 个内容节点/172 链接）、截图 1.2MB。

### 3. 真实扩展路径：Chromium + webbrain MCP server

```bash
node mcp-server 需要先构建：cd webbrain/mcp-server && npm install && npm run build
node dist/index.js pairing-key            # 生成配对密钥（~/.webbrain/bridge-key）

# 用驱动脚本一键完成：起 Chromium + 扩展 → 注入 LLM/Bridge 配置 → 打开页面 → 跑 Ask/Act 任务
WEBBRAIN_EXT=/path/to/webbrain/build/chrome \
WEBBRAIN_MCP=/path/to/webbrain/mcp-server/dist/index.js \
ANTHROPIC_BASE_URL=https://ark.cn-beijing.volces.com/api/coding \
ANTHROPIC_API_KEY=ark-... \
ANTHROPIC_MODEL=doubao-seed-2-1-pro-260628 \
TARGET_URL=https://www.xiaohongshu.com \
node examples/webbrain/drive-webbrain.mjs
```

脚本流程（详见 `drive-webbrain.mjs`）：

1. Playwright 以 `--load-extension` 启动 Chromium（持久 profile）。
2. 向扩展 `chrome.storage.local` 注入 LLM provider 配置（`type: anthropic`）与 Bridge 配置（`webbrainCloudBridge*`）。
3. 打开目标页面，确认内容脚本注入（`window.__generateAccessibilityTree`）。
4. 启动 webbrain MCP server（stdio），扩展从 offscreen document 反向拨号配对（HMAC 配对密钥）。
5. 调 `webbrain_run` 执行真实任务：
   - `mode: ask`——只读分析页面（标题/栏目/登录态/内容列表）；
   - `mode: act`——滚动/点击等交互（受扩展权限门约束，敏感动作需页面内人工确认）。

## 实测结果（2026-11-18，小红书，webbrain v49.1.2 + doubao-seed-2-1-pro）

- **Ask**：读取整页（read_page 至 hasMore:false），输出页面结构分析：未登录态 + 登录弹窗遮罩、「发现→推荐」栏目、13 个频道标签、约 30 条笔记卡片（标题/作者/点赞表格）。
- **Act**：两次滚动（900px → 1800px），观察页面高度 2415px → 2792px 增长，报告滚动后新加载的笔记（标题 + 点赞数）。
- 全链路证据：Chromium 截图（before / act 后）、MCP server 配对日志（capabilities: run_modes_v1、saved_workflows_v1 …）。

## 注意事项

- **扩展安装态**：首次注入配置后 background 通过 `storage.onChanged` 自动重载 provider manager；Bridge 需显式发 `cloud_bridge_start`（仅写 storage 不会拨号）。
- **消息契约**：扩展消息需带 `target: 'background'`，否则 `handleMessage` 不响应。
- **Act 权限门**：删除/付款/发帖等敏感动作会弹页面内确认卡；无人工确认时运行会被取消（`[Stopped by user]`）。纯滚动等低风险动作可自动放行。
- **MCP 先起、再拨号**：先让 MCP server 监听 `ws://127.0.0.1:17374/extension`，扩展的 offscreen 文档再拨号，首次即配对成功。
- **LLM 端点**：webbrain 的 AnthropicProvider 会在 baseUrl 后拼 `/v1/messages`（`https://.../api/coding` → `/api/coding/v1/messages`），并带 `x-api-key` 与 `anthropic-version: 2023-06-01`。
