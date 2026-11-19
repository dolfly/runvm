# WebBrain × runvm — 把开源 AI 浏览器代理的页面能力跑进 runvm

本示例演示如何在 runvm 里获得 [WebBrain](https://github.com/webbrain-one/webbrain)
（开源 AI 浏览器代理）的页面感知与控制能力，并暴露成 MCP 工具给模型统一调用：

1. **runvm 原生 `webbrain` 工具**（推荐）：`h5_open` 带 `"webbrain": true` 打开页面时，自动注入
   WebBrain 扩展的两份内容脚本（`accessibility-tree.js` + `content.js`，原样提取，见
   [`assets/h5/webbrain/`](../../assets/h5/webbrain/README.md)），然后一个 `webbrain` 命令承接
   全部操作——与 WebBrain 扩展 agent 用的是同一份页面代码。
2. **真实扩展路径**（需要 AI 编排时）：编译 webbrain 扩展，在 Chromium 里加载，经 webbrain MCP
   server 委托 Ask/Act 任务（见下节）。

## runvm 原生 webbrain 工具（页面感知 + 控制）

```bash
runvm http -addr 127.0.0.1:41000
# MCP 调用：
#   h5_open   {"target":"https://www.xiaohongshu.com","webbrain":true}
#   webbrain  {"session":"h5-1","action":"snapshot"}      # 页面信息 + 紧凑可访问性树
#   webbrain  {"session":"h5-1","action":"tree","filter":"interactive"}
#   webbrain  {"session":"h5-1","action":"click","ref_id":"ref_7746513088815"}
#   webbrain  {"session":"h5-1","action":"type","ref_id":"ref_7746513088812","text":"..."}
#   webbrain  {"session":"h5-1","action":"scroll","y":900}
#   webbrain  {"session":"h5-1","action":"press","key":"Escape"}
#   h5_close  {"session":"h5-1"}
```

`tree` / `interactive` / `snapshot` 返回带稳定 `ref_id` 的可访问性树与控件清单；`click` / `type` /
`press` / `scroll` / `check` 用 `ref_id` 寻址执行——全部是 webbrain 扩展 agent 在浏览器里使用的
同一套算法。整页导航后工具会自动重新注入。

## 两条路径的边界

| 路径 | 引擎 | 能力 | 用途 |
|---|---|---|---|
| runvm 原生 `webbrain` 工具 | WebKit WebView | 页面感知 + DOM 操作（树/点击/输入/滚动/勾选） | 模型直接驱动的页面自动化，无需浏览器扩展 |
| Chromium + 扩展 + MCP server | Chromium | 完整 AI agent 循环（LLM 规划、多步任务、Ask/Act 模式） | 需要模型自主编排的端到端任务 |

## 真实扩展路径：Chromium + webbrain MCP server

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
