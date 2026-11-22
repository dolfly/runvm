---
version: alpha
name: runvm-console
description: |
  runvm HTTP 控制台的设计系统。视觉主线来自 OpenCode 官网（暖白画布 #fdfcfc、近黑墨色
  #201d1d、hairline 细边框、4px 小圆角、全站无阴影无渐变），补充参照 Linear 的分栏密度与
  键盘优先。与 OpenCode 最关键的一处分歧：**不做全站等宽**。这里是运维工具，人读句子、
  机器读值——标签用无衬线，值（URI / 会话 ID / JSON / schema / 时间戳 / 字节数）用等宽。
  深色只保留给「机器产物且只读」的表面（JSON、代码、报文、终端），浅色承载一切可编辑内容
  与导航。全站离线可用：系统字体栈，零 CDN、零在线图标库、零外部图片。
colors:
  ink: "#201d1d"
  ink-deep: "#0f0000"
  body: "#424245"
  mute: "#646262"
  subtle: "#8a8787"
  disabled: "#b3afae"
  canvas: "#fdfcfc"
  surface-soft: "#f8f7f7"
  surface-card: "#f1eeee"
  hairline: "rgba(15,0,0,0.12)"
  hairline-strong: "#d5d0d0"
  accent: "#0b57d0"
  accent-hover: "#094ab3"
  accent-active: "#073b8f"
  accent-soft: "#e8eefb"
  accent-bright: "#007aff"
  ok: "#1f7a3d"
  warn: "#8a5a00"
  danger: "#b3261e"
  dark-canvas: "#201d1d"
  dark-elevated: "#302c2c"
  dark-hairline: "rgba(253,252,252,0.12)"
  on-dark: "#fdfcfc"
  on-dark-mute: "#9a9898"
  scrim: "rgba(32,29,29,0.32)"
typography:
  page-title:
    fontFamily: ui-sans
    fontSize: 20px
    fontWeight: 600
    lineHeight: 28px
    letterSpacing: 0
  section-title:
    fontFamily: ui-sans
    fontSize: 14px
    fontWeight: 600
    lineHeight: 20px
  body:
    fontFamily: ui-sans
    fontSize: 13px
    fontWeight: 400
    lineHeight: 20px
  prose:
    fontFamily: ui-sans
    fontSize: 14px
    fontWeight: 400
    lineHeight: 22px
  meta:
    fontFamily: ui-sans
    fontSize: 12px
    fontWeight: 400
    lineHeight: 18px
  micro:
    fontFamily: ui-sans
    fontSize: 11px
    fontWeight: 500
    lineHeight: 16px
  mono-code:
    fontFamily: ui-monospace
    fontSize: 12.5px
    fontWeight: 400
    lineHeight: 20px
  mono-data:
    fontFamily: ui-monospace
    fontSize: 12px
    fontWeight: 400
    lineHeight: 18px
spacing:
  "0": 0px
  "1": 4px
  "2": 8px
  "3": 12px
  "4": 16px
  "5": 20px
  "6": 24px
  "8": 32px
  "10": 40px
  "12": 48px
rounded:
  none: 0px
  sm: 4px
  md: 6px
  full: 999px
components:
  topbar:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    height: 48px
    borderBottom: "1px solid {colors.hairline}"
  nav-item:
    textColor: "{colors.mute}"
    typography: "{typography.body}"
    padding: "0 12px"
    height: 48px
  nav-item-active:
    textColor: "{colors.ink}"
    borderBottom: "2px solid {colors.ink}"
  button-primary:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.canvas}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    height: 32px
    padding: "0 14px"
  button-ghost:
    backgroundColor: transparent
    textColor: "{colors.body}"
    border: "1px solid {colors.hairline}"
    rounded: "{rounded.sm}"
    height: 32px
    padding: "0 12px"
  button-danger:
    backgroundColor: transparent
    textColor: "{colors.danger}"
    border: "1px solid {colors.danger}"
    rounded: "{rounded.sm}"
    height: 32px
    padding: "0 12px"
  input:
    backgroundColor: "{colors.surface-soft}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    border: "1px solid {colors.hairline-strong}"
    rounded: "{rounded.sm}"
    height: 32px
    padding: "0 10px"
  table-header:
    backgroundColor: "{colors.surface-soft}"
    textColor: "{colors.mute}"
    typography: "{typography.meta}"
    height: 32px
    borderBottom: "1px solid {colors.hairline-strong}"
  table-row:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.body}"
    typography: "{typography.body}"
    height: 32px
    padding: "0 12px"
  sidebar-item:
    backgroundColor: transparent
    textColor: "{colors.body}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    height: 32px
    padding: "0 8px"
  sidebar-item-active:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.ink}"
  code-block:
    backgroundColor: "{colors.dark-canvas}"
    textColor: "{colors.on-dark}"
    typography: "{typography.mono-code}"
    rounded: "{rounded.none}"
    padding: "12px 14px"
  badge:
    typography: "{typography.micro}"
    rounded: "{rounded.sm}"
    height: 18px
    padding: "0 6px"
  kbd:
    backgroundColor: "{colors.surface-card}"
    textColor: "{colors.mute}"
    typography: "{typography.micro}"
    border: "1px solid {colors.hairline-strong}"
    rounded: "{rounded.sm}"
    height: 18px
    padding: "0 5px"
---

# runvm 控制台设计规格

本文件就是仓库根目录的 `DESIGN.md`：既是设计规范，也是实现依据。研发照它写 CSS 与组件，不需要二次猜布局。

## 0. 怎么读这份文档

- **§2–§5** 是实现层：token → CSS 变量 → 组件尺寸，逐项带数值。
- **§6–§9** 是页面层：信息架构、线框、文件浏览器与调试页的交互细则。
- **§10–§11** 是两条硬约束：响应式断点、中英文 i18n。
- **§12** 是落地映射（变量表、组件清单、知识点），**§13** 是逐条自检表。

一个贯穿全文的判定规则，遇到文档没写到的细节就按它推：

> **值 → 等宽、深色、只读；标签 → 无衬线、浅色、可编辑。**

---

## 1. 参照与取向

### 1.1 主线：OpenCode 官网

取自 `design-template/templates/opencode.ai/DESIGN.md` 的，是这些（原样继承，不重新发明）：

| 继承项 | 值 | 为什么它在 runvm 里也成立 |
|---|---|---|
| 暖白画布 | `#fdfcfc` | 长时间盯屏的底噪比纯白低，比灰白少一层「系统默认感」 |
| 近黑墨色 | `#201d1d` | 文字不用中性黑，整页色调偏暖，和冷蓝强调色形成分工 |
| hairline 边框 | `1px rgba(15,0,0,0.12)` | 用线分区，不用色块分区；信息密度靠线不靠填充 |
| 小圆角 | `4px` | 只有可点元素有圆角，容器一律 0；圆角是「这东西能点」的信号 |
| 无阴影、无渐变 | `box-shadow: none`、`background-image: none` | 层级用线、底色差和遮罩表达，见 §4.4 |
| 「唯一深色卡片」 | `#201d1d` | 在 runvm 里被复用成一条规则，见 §1.4 |

### 1.2 唯一补充参照：Linear

只借两件，且不借它的暗色画布与紫色强调：

1. **多栏密度**：1px hairline 竖线分隔的固定栏（224 / 320 / 1fr），每栏独立滚动，页签不跳转。运维要「左看列表右看内容」，整页跳转会把上下文扔掉。
2. **键盘优先**：`/` 聚焦检索、`j`/`k` 行间移动、`Enter` 打开、`Esc` 关闭、`?` 看快捷键。这在 OpenCode 里是隐含的（终端产品），Linear 把它做成了文档化交互。

参照它是因为：runvm 控制台是**重复性操作**——同一批会话反复看、同一批文件反复查。密度的收益随使用次数线性增长，而营销页那套留白在这里是纯损耗。

### 1.3 明确不借

- 营销式 hero、超大标题（38px+）、装饰性 ASCII wordmark —— 控制台没有「第一屏」。
- 全站等宽（OpenCode 的核心特征），理由见 §1.5。
- 装饰性色块、渐变、插画、品牌色泛滥 —— 强调色全页只出现在「能点 / 有焦点 / 在生效」三处。

### 1.4 深浅关系：一条规则，不是两种主题

**深色 = 机器产物且只读；浅色 = 人可编辑与导航。**

| 表面 | 用色 | 具体位置 |
|---|---|---|
| 画布 / 导航 / 表单 / 表格 / 卡片 | `#fdfcfc` + `#f8f7f7` + `#f1eeee` | 所有可编辑内容和外壳 |
| 深色区 | `#201d1d`（内嵌面 `#302c2c`） | JSON 树与原始报文、代码块、schema、MCP 请求/响应报文、终端式输出 |
| PNG 预览衬底 | `#f8f7f7` + `#e6e2e1` 棋盘格 | 截图预览（避免透明 PNG 看不出边界） |

这样定有实际好处，不是审美：**深色区天然带「只读」语义**，用户在文件编辑器里一眼能分清「这块我改得动」和「这块是远端算出来的」。反过来，整站深色会让可编辑区失去区分度——现状 `--bg: #0f1216` 正是因此所有面板都长得一样。

### 1.5 为什么不做全站等宽

OpenCode 全站等宽成立，因为它全站是英文，且它是终端产品。runvm 两条都不成立：

1. **中文在等宽下没有可靠的等宽字体**。`ui-monospace` 落到中文是系统黑体（比例字体），字宽不一，等宽网格直接崩。要真等宽得随二进制打包 Sarasa Mono SC 这类 CJK 等宽字体，体积 8–20 MB，为一个单二进制 MCP 服务器不值得。
2. **运维工具里，「对齐」的价值只在值上**。人需要对齐的是会话 ID、URI、字节数、时间戳、schema 字段；「实时运行状态」「每 3 秒自动刷新」这类标签不对齐也没人读不出来。

所以分工写死（实现时不要另立规则）：

| 内容 | 字体 | 例子 |
|---|---|---|
| 导航、按钮、表单标签、说明文字、空态、报错、对话框 | 无衬线 `--font-ui` | 「资源管理」「保存」「暂无会话」 |
| URI、会话 ID、工具名、参数名、JSON 键值、schema、时间戳、字节数、sha256、版本号、快捷键、MCP 方法名、状态码 | 等宽 `--font-mono` | `runvm://files/report.md`、`h5_console`、`12ms`、`201` |

判定口诀：**这个词是仓库造的还是人写的？仓库造的用等宽。**

---

## 2. 色彩 token

### 2.1 基础色板

| CSS 变量 | 值 | 用途 | 在 `#fdfcfc` 上的对比度 |
|---|---|---|---|
| `--c-canvas` | `#fdfcfc` | 画布、卡片、表格行底色 | — |
| `--c-surface-soft` | `#f8f7f7` | 输入框底、表头、行 hover、预览衬底 | — |
| `--c-surface-card` | `#f1eeee` | 侧栏选中、kbd、次级分组 | — |
| `--c-ink` | `#201d1d` | 标题、主文字、primary 按钮底 | 16.5:1 |
| `--c-ink-deep` | `#0f0000` | 深色区最暗、分隔强调 | — |
| `--c-body` | `#424245` | 正文、表格单元格 | 9.6:1 |
| `--c-mute` | `#646262` | 说明文字、表头、占位 | 5.9:1 |
| `--c-subtle` | `#8a8787` | 禁用文字、单位后缀、极弱元信息 | 3.5:1（仅非必要信息） |
| `--c-disabled` | `#b3afae` | 禁用控件边框与文字 | — |
| `--c-hairline` | `rgba(15,0,0,0.12)` | 全部分隔线、卡片边框 | — |
| `--c-hairline-strong` | `#d5d0d0` | 表头下沿、输入框边框、浮层边框 | — |

### 2.2 强调色与语义色

| CSS 变量 | 值 | 用途 | 对比度 |
|---|---|---|---|
| `--c-accent` | `#0b57d0` | 链接、选中的 tab 下划线、焦点环、live 指示 | 6.3:1 |
| `--c-accent-hover` | `#094ab3` | hover | 7.7:1 |
| `--c-accent-active` | `#073b8f` | active / 按下 | 9.7:1 |
| `--c-accent-soft` | `#e8eefb` | 选中行底、检索命中高亮底 | — |
| `--c-accent-bright` | `#007aff` | **仅深色区**的链接与焦点环 | — |
| `--c-ok` | `#1f7a3d` | 就绪、成功 | 5.3:1 |
| `--c-warn` | `#8a5a00` | 警告、资源缺失、lint warning | 5.8:1 |
| `--c-danger` | `#b3261e` | 错误、危险按钮文字 | 6.4:1 |
| `--c-ok-soft` | `#e6f2e9` | 成功 pill 底 | — |
| `--c-warn-soft` | `#fdf3e0` | 警告 pill 底 | — |
| `--c-danger-soft` | `#fbeceb` | 错误 pill / 错误态卡片底 | — |

强调色使用纪律（评审会按这条卡）：`--c-accent` 只允许出现在——链接文字、`aria-selected` 的 tab 下划线、`:focus-visible` 焦点环、状态条的 live 圆点、检索命中高亮。**不允许**做成大面积背景、不允许给卡片描边、不允许用于「强调标题」。

### 2.3 深色区色板

| CSS 变量 | 值 | 用途 | 在 `#201d1d` 上的对比度 |
|---|---|---|---|
| `--c-dark` | `#201d1d` | 代码/JSON/报文底 | — |
| `--c-dark-elevated` | `#302c2c` | 深色区内嵌面（请求报文标题栏、选中行） | — |
| `--c-dark-line` | `rgba(253,252,252,0.12)` | 深色区分隔线、内嵌边框 | — |
| `--c-on-dark` | `#fdfcfc` | 深色区主文字、JSON 键 | 16.5:1 |
| `--c-on-dark-mute` | `#9a9898` | 深色区标点、行号、次级 | 5.9:1 |
| `--c-syn-string` | `#9ccc9c` | JSON 字符串值 | 8.1:1 |
| `--c-syn-number` | `#ffb86b` | 数字 / 布尔 / null | 10.2:1 |
| `--c-ok-bright` | `#30d158` | 深色区成功点 | 8.3:1 |
| `--c-warn-bright` | `#ff9f0a` | 深色区警告点 | 9.0:1 |
| `--c-danger-bright` | `#ff3b30` | 深色区错误点 | 4.7:1 |

语法着色只有 4 档（键 = 白、字符串 = 绿、数字/布尔/null = 琥珀、标点 = 灰）。**不做 8 色彩虹高亮**：读 JSON 找的是结构不是配色，颜色多了反而要重新学一遍颜色含义。

### 2.4 遮罩与浮层

| CSS 变量 | 值 |
|---|---|
| `--c-scrim` | `rgba(32,29,29,0.32)` |

对话框/命令面板用 `--c-canvas` 底 + `1px solid --c-hairline-strong` + 遮罩 `--c-scrim` + **`box-shadow: none`**。层级靠遮罩和线表达，不靠投影。

---

## 3. 字体与排版

### 3.1 字体栈（全部系统字体，零网络请求）

```css
--font-ui: system-ui, -apple-system, "Segoe UI", "PingFang SC", "Hiragino Sans GB",
           "Noto Sans SC", "Microsoft YaHei", sans-serif;
--font-mono: ui-monospace, "SF Mono", SFMono-Regular, "JetBrains Mono", Menlo,
             Consolas, "Cascadia Mono", "Liberation Mono", monospace;
--font-mono-cjk: "Sarasa Mono SC", "Noto Sans Mono CJK SC", ui-monospace,
                 "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", monospace;
```

- 不引 Google Fonts / 不 `@font-face` 远端地址 / 不用 CDN 图标库。图标一律用 Unicode 几何字符（`▸ ▾ ▾ × ← → ↑ ↓ ● ○ ⌘`）或内联 SVG（`<svg>` 直接写在 HTML 里，1 处不到 1 KB）。控制台要内嵌进单个二进制、可能跑在内网，任何外链都是故障点。
- 若未来确实要 Berkeley Mono 式的英文等宽，只在仓库内打包 `woff2`（子集化，单文件 < 60 KB），并配 `font-display: swap` + 上述栈做回退。**本单不要求打包字体。**

### 3.2 字号阶

| token | CSS 变量 | 字号/行高 | 字重 | 用途 |
|---|---|---|---|---|
| page-title | `--fs-title` | `20px/28px` | 600 | 页面标题（每页一个） |
| section-title | `--fs-section` | `14px/20px` | 600 | 卡片头、面板标题、区块标题 |
| body | `--fs-body` | `13px/20px` | 400 | UI 默认：导航、按钮、表格、表单 |
| prose | `--fs-prose` | `14px/22px` | 400 | 说明段落、空态正文、文档型内容 |
| meta | `--fs-meta` | `12px/18px` | 400 | 表头、时间戳、字节数、辅助说明 |
| micro | `--fs-micro` | `11px/16px` | 500 | pill、kbd、单位、计数徽标 |
| mono-code | `--fs-mono` | `12.5px/20px` | 400 | JSON、代码、报文 |
| mono-data | `--fs-mono-sm` | `12px/18px` | 400 | 表格里的 URI / ID / 数字 |

字重只有 400 / 500 / 600 三档。**禁用 700**：中文在 13px 下 700 会糊成一团，600 已足够表达层级。

### 3.3 层级与间距规则

- 标题与正文之间 `8px`；区块之间 `24px`；同一区块内的行 `0`（靠行高）。
- 标题**不用渐变色、不用下划线、不加徽标色块**；层级只靠字号 + 字重 + 上下留白。
- 数字与单位之间留 `4px`（`12.5 MiB`、`320 ms`），数字一律用等宽。
- 时间戳格式固定 `2026-11-22T13:25:04Z`（等宽，不本地化数字形态，见 §11.6），旁边可加相对时间 `3 分钟前 / 3 min ago`（无衬线，12px，`--c-mute`）。

### 3.4 中文排版处理

| 规则 | 值 |
|---|---|
| 中文字号 | 与英文同字号。中文在 13px 可读，**不因为「中文看着小」而整体放大**（会破坏基线） |
| 中文行高 | `20px`（13px 字号下 1.54）。英文行高同为 `20px`——统一基线，避免两种语言切换时页面跳动 |
| 中文大标题行高 | `1.4`（20px 标题 → 28px） |
| 英文大标题行高 | `1.3` + `letter-spacing: -0.01em` |
| 中英混排间距 | 不手动加空格字符。用 CSS `text-spacing-trim`（支持则生效）或不做——中文与等宽英文之间天然有视觉间隙 |
| 中文加粗 | 600 上限，12px 以下禁止加粗（改用一个 `--c-mute` 的次级色区分） |
| 中文标点 | 用全角中文标点；括号内是英文/代码时用半角。`「」` 用作界面上的引述，`（）` 用作补充说明 |
| 禁止 | `text-transform: uppercase`（对中文无效，对英文会把 `runvm` 变成 `RUNVM`——品牌名与 URI 会失真） |

### 3.5 等宽字体在中文下怎么落

这是本单必须写死的一条，否则实现时必然出问题：

1. 等宽只用于**值是英文/数字/符号**的位置。这些位置上的中文（罕见，例如文件名的中文部分 `报告-2026.md`）用 `--font-mono-cjk` 回退，允许不齐。
2. **任何对齐都不得依赖字符宽度**。表格用 CSS Grid / `<table>` 的列宽，不用空格填充或 `padding-left` 对齐字符；JSON 树用嵌套 `<div>` + `padding-left: 16px/层`，不用缩进空格。
3. 等宽 + 中文混排的行，行高固定 `20px`，禁止 `line-height: normal`（中文回退字体的默认行高会比英文高，导致行高抖动）。
4. 代码块里出现中文注释时，`--font-mono-cjk` 的 `font-size` 保持同值，**不缩小**（缩小会让中文在该行视觉上塌陷）。

---

## 4. 空间与形状

### 4.1 间距阶（4px 基准）

```css
--s-0: 0px;   --s-1: 4px;  --s-2: 8px;   --s-3: 12px;  --s-4: 16px;
--s-5: 20px;  --s-6: 24px; --s-8: 32px;  --s-10: 40px; --s-12: 48px;
```

只允许用这 10 个值。**禁止 10px / 14px / 18px 之类的经验值**——密度靠这套阶控制，掺入散值后整页会失去节奏。

| 场景 | 值 |
|---|---|
| 图标与文字 | `--s-2` (8px) |
| 控件内水平 padding | `--s-3` (12px)；输入框 `--s-3` 左侧、`--s-2` 右侧（给清除按钮留位） |
| 表单字段间 | `--s-3` (12px) |
| 表格单元格水平 padding | `--s-3` (12px)；首列 `--s-4` |
| 卡片内 padding | `--s-4` (16px)；卡片头与内容间 `--s-3` |
| 卡片组 gap | `--s-4` (16px) |
| 页面内容 padding | 上 `--s-6` (24px)、下 `--s-10` (40px)、左右 `--s-8` (32px) |
| 区块之间 | `--s-6` (24px) |
| 侧栏内 padding | `--s-2` (8px) |

### 4.2 圆角

```css
--r-none: 0px; --r-sm: 4px; --r-md: 6px; --r-full: 999px;
```

| 元素 | 圆角 |
|---|---|
| 卡片、面板、表格、深色代码块、栏容器、图片预览框 | `0` |
| 按钮、输入框、下拉、侧栏项、tab 项、pill、kbd、行选中态 | `4px` |
| 对话框、命令面板、toast、抽屉 | `6px` |
| 状态点（live 指示、后端可用性） | `999px` |
| 头像/图标容器 | `4px` |

### 4.3 边框

- 一切分隔：`1px solid var(--c-hairline)`。
- 需要更强分隔（表头下沿、输入框、浮层外框）：`1px solid var(--c-hairline-strong)`。
- 焦点：`outline: 2px solid var(--c-accent); outline-offset: 1px;`（**不用 box-shadow 画焦点环**，见下条）。
- 选中态：`1px solid var(--c-hairline)` 底 + `2px` 左边框 `--c-accent`（用在表格行、列表项）。
- 深色区：`1px solid var(--c-dark-line)`。

### 4.4 阴影：全站 0

```css
--shadow-none: none;
```

**本单沿用 OpenCode 的「无阴影」**，不做任何 `box-shadow`（含焦点环、含 toast、含对话框）。层级用三层机制表达：

| 层 | 表达方式 | z-index |
|---|---|---|
| L0 画布 | `--c-canvas` | 0 |
| L1 面板 | `1px --c-hairline` 分隔，无底色差 | 1 |
| L2 浮层（对话框 / 命令面板 / toast / 抽屉） | `--c-canvas` 底 + `1px --c-hairline-strong` + `--c-scrim` 遮罩 | 100/110 |

理由：runvm 控制台的浮层只有「确认删除/覆盖」「命令面板」「toast」三类，出现频率低；阴影在深色背景旁会产生脏边，而这套系统里深色区（JSON）紧挨浅色浮层是高频场景。

### 4.5 关键尺寸

| 元素 | 高/宽 |
|---|---|
| 顶栏 | `48px` |
| 二级标签栏（窄屏） | `40px` |
| 表格行（默认密度） | `32px` |
| 树行 / 列表行（文件、方法） | `28px` |
| 侧栏项 | `32px` |
| 按钮 | `28px`（小/工具栏内）/ `32px`（默认）/ `36px`（表单主操作） |
| 输入框 | `32px`；textarea 最小 `120px` |
| 检索框（检索首页） | `44px` |
| pill / 徽标 | `18px` |
| kbd | `18px` |
| 顶栏语言切换 | `28px`，总宽 `88px` |
| 资源管理侧栏 | `224px` |
| 文件页目录树 | `224px`；文件列表 `320px` |
| 工具页列表 | `360px` |
| 截图页列表 | `280px` |
| 调试页左栏 | `240px` |
| 表单/详情阅读列最大宽 | `880px` |
| 最小可用宽度 | `768px` |

---

## 5. 组件规范

每个组件给：尺寸、间距、状态、用到的 token。**所有状态都必须实现**，不能只做默认态——运维工具的信任来自「失败时它说清楚了」。

### 5.1 按钮

| 变体 | 底色 | 文字 | 边框 | 用途 |
|---|---|---|---|---|
| primary | `--c-ink` | `--c-canvas` | 无 | 每屏最多 1 个：登录、保存、发起调用 |
| ghost（次级） | 透明 | `--c-body` | `1px --c-hairline` | 刷新、取消、下载、打开原图 |
| danger | 透明 | `--c-danger` | `1px --c-danger` | 删除、覆盖保存、清空 |
| link | 无 | `--c-accent` | 无（hover 加下划线） | 行内跳转（「查看详情」） |
| icon | 透明 | `--c-mute` | 无 | 24×24 点按区，图标 14×14 |

尺寸：`height: 32px; padding: 0 14px; font: 500 13px/20px var(--font-ui); border-radius: 4px; gap: 8px;`（icon 按钮：`28×28`，padding 0；工具栏内按钮 `28px` 高，padding `0 10px`）

状态：

| 状态 | 表现 |
|---|---|
| hover | primary → `--c-ink` 不变，`opacity: 0.88`；ghost → `border-color: --c-hairline-strong`，`background: --c-surface-soft`；danger → `background: --c-danger-soft` |
| active | `transform: none`（不做缩放），primary → `--c-ink-deep` |
| focus-visible | `outline: 2px solid var(--c-accent); outline-offset: 1px` |
| disabled | `color: --c-disabled; border-color: --c-hairline; background: --c-surface-soft; cursor: not-allowed;` 深色区 primary 禁用：`background: --c-dark-elevated; color: --c-on-dark-mute` |
| 加载中 | 文字左移 20px 位放 `16×16` 旋转方框（`1px 边框 + border-top-color: 当前文字色`，`600ms linear infinite`），**按钮宽度不变**，`pointer-events: none` |

### 5.2 输入框 / textarea / select / 检索框

```
height 32px | padding 0 10px | border 1px --c-hairline-strong | radius 4px
background --c-surface-soft | color --c-ink | font 400 13px/20px --font-ui
placeholder --c-subtle | 中文 placeholder 与英文同字号
```

- focus：`border-color: --c-accent` + `outline: 2px solid var(--c-accent); outline-offset: 0`（双保险，边框色单独不足以满足非文本提示场景）
- error：`border-color: --c-danger`，下方 `12px/18px` 错误文本（`--c-danger`），`margin-top: 4px`，带 `role="alert"`
- disabled：`background: --c-surface-card; color: --c-disabled`
- textarea：`min-height: 120px; padding: 8px 10px; font-family: var(--font-mono); font-size: 12.5px; line-height: 20px; resize: vertical`（JSON / md / txt 编辑都用 textarea，不自造富文本编辑器）
- select：高 32px，右侧 `▾` 用 12px 文本字符，`appearance: none`，padding-right `24px`
- 检索框（两种形态，样式一致，高度不同）：
  - 工具栏检索：`height: 28px`，左侧 `⌕` 字符 12px `--c-mute`，点击区整个输入框
  - 检索首页检索框：`height: 44px; font-size: 14px; padding: 0 14px 0 40px;`，左侧放大镜 16px，右侧 `kbd` 提示 `/`
- 清除按钮：有值时输入框右侧出现 `×`（16×16，`--c-mute`，hover `--c-ink`），padding-right 由 10px 增至 28px

### 5.3 分段控件（语言切换 / 视图切换）

```
容器: height 28px; border 1px --c-hairline; radius 4px; padding 2px; background transparent
项:   height 24px; padding 0 8px; radius 2px; font 500 11px/16px --font-ui; color --c-mute
选中项: background --c-surface-card; color --c-ink; border-bottom none
```

语言切换放顶栏右侧，总宽 `88px`（两段各 44px），文案 `中文` / `EN`（两个语言下**都不切换**，永远显示各自的自称——用户切错语言时还能认出来）。

### 5.4 顶栏与导航项

```
顶栏: height 48px; padding 0 16px; background --c-canvas; border-bottom 1px --c-hairline
      display: grid; grid-template-columns: auto 1fr auto; gap 16px; align-items: center
左: 品牌 "runvm"（等宽 13px/600 --c-ink） + 版本 pill（mono-data 12px --c-mute）
中: 导航项，gap 4px
右: live 状态点 · 语言分段 · 退出（ghost, 28px）
```

导航项：

| 状态 | 表现 |
|---|---|
| 默认 | `height: 48px; padding: 0 12px; color: --c-mute; font 400 13px/20px` |
| hover | `color: --c-ink`；`border-bottom: 2px solid --c-hairline-strong` |
| 选中 | `color: --c-ink; font-weight: 600; border-bottom: 2px solid --c-ink` |
| focus-visible | `outline: 2px solid --c-accent; outline-offset: -4px` |

**下划线用墨色而不是强调蓝**：强调蓝留给「正在生效的动作」，导航选中是位置不是动作，用墨色更稳。这是本系统相对 OpenCode 保持一致性、同时避免蓝色泛滥的具体取舍。

### 5.5 侧栏项（资源管理二级导航）

```
容器: width 224px; padding 8px; border-right 1px --c-hairline
项:   height 32px; padding 0 8px; radius 4px; gap 8px;
      font 400 13px/20px; color --c-body
图标: 14×14 等宽字符（⌕ 检索 / ▦ 工具 / ▣ 截图 / ▤ 文件），--c-mute
```

| 状态 | 表现 |
|---|---|
| hover | `background: --c-surface-soft` |
| 选中 | `background: --c-surface-card; color: --c-ink; font-weight: 500` |
| 选中 + 有子计数 | 右侧 `pill`：`12px` 等宽 `--c-mute` |

侧栏底部固定一行 `12px/18px --c-subtle`：「资源根：`<path>`」——等宽，超长省略 + `title` 全量。

### 5.6 表格

```
容器: border 1px --c-hairline; radius 0; background --c-canvas
表头: height 32px; background --c-surface-soft; border-bottom 1px --c-hairline-strong;
      font 500 12px/18px --font-ui; color --c-mute; padding 0 12px; text-align: left
行:   height 32px; border-bottom 1px --c-hairline; padding 0 12px;
      font 400 13px/20px --font-ui; color --c-body
```

- 末行无下边框（`tr:last-child td { border-bottom: none }`）。
- 数字列 / ID 列 / URI 列：`font-family: var(--font-mono); font-size: 12px`，`text-align: right`（数字）或 `left`（URI/ID）。
- 列宽：`<colgroup>` 显式声明；文本列 `min-width: 120px`，URI 列 `min-width: 240px`，操作列 `width: 96px; text-align: right`。
- hover：`background: --c-surface-soft`。
- 选中行：`background: --c-accent-soft` + `border-left: 2px solid --c-accent` + `padding-left: 10px`（补偿左边框）。
- 排序：表头文字后 `▾`/`▴`（10px，`--c-mute`），点击循环 升 → 降 → 无。
- 空态：见 §5.13，`<tbody>` 内一行 `td colspan=all`，高 `96px`。
- 加载态：见 §5.14，用 3 条骨架行，每行高 32px。
- **禁用斑马纹**：hover 与选中态已在同一色系，斑马纹会把 32px 的密集行切碎。

### 5.7 卡片

```
border 1px --c-hairline; radius 0; background --c-canvas; padding 16px
卡片头: height 20px; font 600 14px/20px; color --c-ink; margin-bottom 12px
        右侧 meta: font 400 12px/18px --c-mute
```

- 卡片**不做可点整体**（不做整卡 hover 提升），点击目标只放在卡片内的按钮/链接上。
- 唯一例外是「后端卡」（总览页）：整卡不可点，但内部有「查看会话」链接跳到筛选后的会话表。
- 卡片组：`display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px;`

### 5.8 深色代码块（JSON / schema / 报文）

```
background --c-dark; color --c-on-dark; radius 0;
padding 12px 14px; font 400 12.5px/20px var(--font-mono)
max-height 360px; overflow auto; border 1px --c-dark-line
```

- 头部条（可选，报文/请求响应必用）：`height: 28px; background: --c-dark-elevated; font 500 11px/16px var(--font-ui); color: --c-on-dark-mute; padding: 0 14px; display: flex; gap 8px; align-items: center`，右侧放「复制」icon 按钮（14×14，`--c-on-dark-mute`）。
- 行号（启用时）：`width: 32px; text-align: right; padding-right: 12px; color: --c-on-dark-mute; user-select: none`。
- 滚动条：宽 `8px`，轨道透明，thumb `rgba(253,252,252,0.20)`，hover `0.32`，radius 4px（浅色区 thumb 用 `--c-hairline-strong`）。
- 复制成功后按钮文案替换为 `✓` 持续 `1200ms`。
- 语法色见 §2.3，只有 4 档。

### 5.9 JSON 树视图（文件浏览器 / 报文区专用）

```
容器: 深色区同 5.8；每层 padding-left 16px；行高 20px
折叠箭头: ▸（闭合）/ ▾（展开），12px，--c-on-dark-mute，点击区 16×16
键:   --c-on-dark；字重 500；后接 ": "
字符串: --c-syn-string
数字/布尔/null: --c-syn-number
标点 {}[] , : --c-on-dark-mute
```

- 顶层默认展开到第 2 层；超过 200 个节点的数组默认折叠并显示 `… 还有 N 项`（`--c-on-dark-mute`，行高 20px，可点展开）。
- 键名傍边可显示类型徽标（仅调试页的 schema 检查区用）：`11px` 等宽 `--c-on-dark-mute`。
- 焦点：键盘可聚焦节点，`:focus-visible { outline: 1px solid var(--c-accent-bright); outline-offset: -1px; }`
- 搜索命中：`background: rgba(255,184,107,0.18)`（深色区）/ `--c-accent-soft`（浅色区）。

### 5.10 标签页（结果区 / 文件编辑器）

```
容器: height 32px; border-bottom 1px --c-hairline; gap 16px; padding 0 0
项:   height 32px; padding 0; font 400 13px/20px; color --c-mute
选中: color --c-ink; font-weight 600; border-bottom 2px solid --c-ink
hover: color --c-ink
计数: 选中/默认项后接 12px 等宽 --c-mute，数字与括号： "结果 (3)"
```

`role="tablist"` + `aria-selected`，方向键切换。**不用圆角药丸 tab**——那会让密集界面出现过多形状。

### 5.11 面包屑

```
height 20px; font 400 12px/18px var(--font-ui); color --c-mute; gap 6px
分隔符 "/" --c-subtle
当前项: color --c-ink; font-weight 500
文件名段: font-family var(--font-mono); font-size 12px
```

- 段数 > 4 时折叠为 `runvm://files/ … /docs/ report.md`，中间段点击弹出下拉（`6px` 圆角 + `--c-hairline-strong` 边框）。
- 文件页面包屑固定在工具栏左侧，`overflow: hidden; text-overflow: ellipsis; white-space: nowrap`。

### 5.12 徽标 / pill

| 变体 | 底 | 文字 | 边框 | 用途 |
|---|---|---|---|---|
| neutral | `--c-surface-card` | `--c-body` | 无 | 计数、`GET`/`POST`、后端名 |
| ok | `--c-ok-soft` | `--c-ok` | 无 | 就绪、成功、只读 |
| warn | `--c-warn-soft` | `--c-warn` | 无 | 资源缺失、warning 计数 |
| danger | `--c-danger-soft` | `--c-danger` | 无 | 错误、阻断 |
| outline | 透明 | `--c-mute` | `1px --c-hairline` | 中性标签（`writable`、`readonly`） |

```
height 18px; padding 0 6px; radius 4px; font 500 11px/18px --font-ui
等宽内容（GET/数字/后端名）: font-family --font-mono; font-size 11px
```

状态点（内嵌在 pill 或文字前）：`6×6`，`border-radius: 999px`，颜色取对应 `--c-*-bright`/`--c-*`，`margin-right: 6px`。

### 5.13 kbd

```
height 18px; min-width 18px; padding 0 5px; radius 4px;
background --c-surface-card; border 1px --c-hairline-strong; border-bottom-width 1px;
font 500 11px/18px var(--font-mono); color --c-mute
```

组合键用 `+` 分隔的多个 kbd，`gap: 2px`（`⌘` `K`）。

### 5.14 空态 / 加载态 / 错误态

三态是同一块容器（`min-height: 96px`；整页级 `240px`），垂直居中，内部结构固定：

**空态**

```
高度 96px（表格内）/ 240px（页面级）
图标: 24×24 等宽字符（▤ 无文件 / ⌕ 无结果 / ▦ 无工具），--c-subtle
标题: 13px/20px 500 --c-body（"暂无文件"）
说明: 12px/18px --c-mute，最大宽度 320px，居中，1–2 行
动作: ghost 按钮 28px（可选，如「上传第一个文件」）
垂直间距: 图标→标题 12px，标题→说明 8px，说明→动作 16px
```

**加载态**

- 表格：3 条骨架行，高 32px，每行由 3 个灰块组成（宽 120 / 200 / 80px，高 12px，`background: --c-surface-card`，`animation: shimmer 1200ms linear infinite`，`opacity: 0.6 → 1`）。
- 面板/卡片：`48px` 高骨架块 ×2。
- 首次加载整页：顶栏下 `2px` 进度条（`--c-accent`，`animation: indeterminate 900ms linear infinite`），**不做全屏遮罩**——运维要能看见底下的旧数据。
- 文本型加载：`加载中… / Loading…`，12px `--c-mute`，仅用于 <200ms 的局部刷新。

**错误态**

```
背景: --c-danger-soft（区块级）/ --c-canvas（页面级 + 1px --c-hairline）
左侧: 2px 竖条 --c-danger
padding: 12px 14px
标题: 13px/20px 600 --c-danger（"加载失败"）
正文: 12px/18px --c-body；技术细节（HTTP 状态、后端原始 error）用等宽 12px 单独一行
动作: ghost「重试」，28px，右侧
```

规则：**保留原始技术信息，不加「请稍后重试」这类空话**。后端返回什么就显示什么（`invalid credentials`、`401`、`413 request body too large`），中文只包裹外层标题。这符合仓库「不猜、不装」的口径。

### 5.15 对话框 / 命令面板 / toast

| 元素 | 尺寸 |
|---|---|
| 对话框 | 宽 `480px`（确认类）/ `640px`（表单类），padding `20px`，radius `6px`，边框 `1px --c-hairline-strong`，遮罩 `--c-scrim` |
| 对话框标题 | 16px/24px 600，下方 8px；正文 13px/20px `--c-body`；按钮区右对齐，gap 8px，上间距 20px |
| 命令面板 | 宽 `560px`，顶部 20vh，输入框 44px（无边框，底部 1px hairline），结果区最大高 `360px`，行 32px |
| toast | 右下 `24px` 定位，宽 `320px`，padding `10px 12px`，radius `6px`，边框 `1px --c-hairline-strong`，`--c-canvas` 底 + 左侧 `2px` 语义色；`5s` 自动消失，hover 暂停 |

toast 状态色只在左侧竖条上，正文始终 `--c-ink`（避免整块染色后读不清）。

---

## 6. 信息架构与导航

### 6.1 结构树

```
runvm 控制台
├── 系统总览  /                                   后端可用性 · 限额 · 活跃会话
├── 资源管理  /resources
│   ├── 检索       /resources                     跨类型检索首页（默认落地页）
│   ├── 工具       /resources/tools               runvm://tools 的 24 个工具 + input schema
│   ├── 截图       /resources/screenshots         runvm://screenshot/<session> 的 PNG
│   └── 文件       /resources/files               runvm://files/<相对路径>，内置文件浏览器 + 上传
└── MCP调试   /debug                              只读调试，对齐 Inspector 的信息分区
```

### 6.2 每一项的功能边界（实现时不得越界）

| 页面 | 读什么 | 写什么 |
|---|---|---|
| 系统总览 | `GET /api/status`（version / go / os / arch / 各后端 sessions / limits / sessions 明细 / reclaimed） | 无 |
| 资源管理-检索 | `GET /api/resources` + 三类资源的元信息 | 无 |
| 资源管理-工具 | `runvm://tools`（24 工具 + input schema） | 无 |
| 资源管理-截图 | `runvm://screenshot/<session>`（PNG） | 无 |
| 资源管理-文件 | `GET /api/resources`、`GET /api/resources/read?uri=` | `PUT /api/resources/file`（保存）、`POST /api/upload`（上传） |
| MCP 调试 | `GET /api/debug/catalog`、`GET /api/debug/lint`、`POST /api/debug`（只读方法 + 6 个只读工具） | 无（物理上没有写入口） |

**上传不再单独做一个顶层 tab**：上传的产物就是 `runvm://files/*`，它属于「文件」页的一次写入操作。放成顶层 tab 会让用户以为上传是独立功能，而实际上传完还要跳到资源页才能看见结果——这是现状 4 个平级 tab 里最不好用的一处。文件页的主操作区（工具行右侧 + 空态按钮 + 拖拽整个列表区）承接上传。

### 6.3 URL 与刷新行为

- 每个页面有独立路径（见 6.1），`history.pushState` 切换，**不做整页刷新**；浏览器前进/后退可用。
- 语言与页面状态不同步到路径（语言走持久化偏好，见 §11.1）。
- 文件页的当前路径与选中文件同步到 query：`/resources/files?path=docs&file=report.md`，可直接分享/刷新还原。
- 截图页：`/resources/screenshots?session=<id>`。
- 调试页：`/debug?method=tools/list`（其余状态不持久化）。
- 未登录访问任意路径 → 渲染登录页，登录成功后回到原路径（`?next=`）。

### 6.4 键盘地图（全局，实现清单）

| 按键 | 作用 | 生效范围 |
|---|---|---|
| `/` | 聚焦当前页检索框 | 所有页 |
| `Esc` | 清空检索 → 关闭浮层 → 取消编辑态（逐级） | 所有页 |
| `j` / `k` 或 `↓` / `↑` | 列表/结果行移动 | 有列表的页 |
| `Enter` | 打开选中项 | 列表/检索/文件树 |
| `g` 然后 `o` / `r` / `d` | 跳到 总览 / 资源管理 / 调试 | 所有页 |
| `r` | 刷新当前视图数据 | 所有页（输入框聚焦时不生效） |
| `?` | 打开快捷键面板（命令面板复用同一浮层） | 所有页 |
| `⌘/Ctrl + S` | 保存当前编辑中的文件 | 文件编辑态 |
| `⌘/Ctrl + Enter` | 发起当前调试调用 | 调试页参数区 |

实现要求：快捷键在 `document` 上监听，输入类元素聚焦时**只放行** `Esc`、`⌘S`、`⌘Enter`。

---

## 7. 页面线框

坐标系：所有数字为 px；`│`/`─` 为 1px hairline 分隔，不是装饰。

### 7.1 登录页

```
┌────────────────────────────────────────── 100vh ──────────────────────────────────────────┐
│                                                                                           │
│                              ┌─ 表单列 360px，垂直居中（距顶 24vh）─┐                      │
│                              │ runvm                             │  mono 20px/600        │
│                              │ 控制台  ·  Console                 │  13px --c-mute        │
│                              │                                   │  间距 24px            │
│                              │ 用户名                            │  12px/18px --c-mute   │
│                              │ ┌───────────────────────────────┐ │  输入框 36px 高       │
│                              │ └───────────────────────────────┘ │  间距 12px            │
│                              │ 密码                              │                       │
│                              │ ┌───────────────────────────────┐ │                       │
│                              │ └───────────────────────────────┘ │  间距 20px            │
│                              │ ┌───────────────────────────────┐ │  primary 36px，满宽   │
│                              │ │            登录                │ │                       │
│                              │ └───────────────────────────────┘ │  间距 12px            │
│                              │ invalid credentials               │  danger 12px/18px     │
│                              │                                   │                       │
│                              │ ─────────── 1px hairline ───────── │  上间距 24px          │
│                              │ runvm v0.8.2 · go1.24 linux/amd64 │  11px 等宽 --c-subtle │
│                              └───────────────────────────────────┘                      │
└───────────────────────────────────────────────────────────────────────────────────────────┘
```

- 表单列无卡片边框、无底色（画布即卡片），只靠 24px 的留白和 hairline 分组——登录页是唯一不需要密集信息的页面。
- 提交中：按钮进入加载态，输入框 `disabled`。
- 失败：错误文本出现在按钮下方，`role="alert"`，输入框不清空（重输密码除外），焦点回到密码框。

### 7.2 系统总览 `/`

```
┌ 顶栏 48px ─────────────────────────────────────────────────────────────────────────────────┐
│ runvm  v0.8.2  │ 系统总览  资源管理  MCP调试 │        ● 每 3s  │ 中文|EN │ 退出 │
├────────────────────────────────────────────────────────────────────────────────────────────┤
│ padding 24px 32px                                                                           │
│ 系统总览                                                        [刷新] [自动刷新 ●]          │ 标题 20/28 600；右侧 2 个 ghost 28px
│ 后端、限制与活跃会话。每 3 秒自动刷新。                                                     │ 13/20 --c-mute，上间距 4px
│ ← 24px →                                                                                    │
│ ┌ 后端卡 3 列 grid, gap 16 ──────────────────────────────────────────────────────────────┐ │
│ │ ┌ Android ───────────────┐ ┌ iOS ────────────────────┐ ┌ H5 ─────────────────────┐   │ │
│ │ │ ● 可用      [ok]       │ │ ● 可用       [ok]       │ │ ● 可用       [ok]       │   │ │  卡高 108px, padding 16
│ │ │ 2 会话                 │ │ 0 会话                  │ │ 1 会话                  │   │ │  数字 24px/32px 600 等宽
│ │ │ android                  │ │ ios                     │ │ h5                      │   │ │  后端名 12px 等宽 --c-mute
│ │ │ ~/.runvm/assets  就绪   │ │ macho arm64 dylibs;     │ │ —                       │   │ │  附注 11px/16px --c-mute, 2 行截断
│ │ │ 查看会话 →             │ │ 查看会话 →              │ │ 查看会话 →              │   │ │  link 12px，底对齐
│ │ └────────────────────────┘ └─────────────────────────┘ └─────────────────────────┘   │ │
│ └────────────────────────────────────────────────────────────────────────────────────────┘ │
│ ← 24px →                                                                                    │
│ ┌ 限额卡 3 列 grid, gap 16 ─────────────────────────────────────────────────────────────┐ │
│ │ 会话上限  [0 = 不限]     │ 空闲超时  30m0s          │ 已回收  0 次                  │ │  同卡样式，值 20px/28px 等宽
│ └────────────────────────────────────────────────────────────────────────────────────────┘ │
│ ← 24px →                                                                                    │
│ 活跃会话                                          [筛选: 全部 ▾]  [检索会话…]             │ 区块标题 14/20 600 + 28px 控件
│ ┌ 表格 ────────────────────────────────────────────────────────────────────────────────┐  │
│ │ 后端     │ 会话 ID                            │ 池        │ 引擎 in_use/idle/created  │  │ 表头 32px --c-surface-soft
│ │ android  │ 1f3a9c0e-6b21-4f7a-9d33-2c88a0b4e5c1 │ native    │ 1 / 3 / 4                 │  │ 行 32px；ID 列 12px 等宽
│ │ h5       │ 7c2e5d90-4a11-4b3f-8e1a-7b0d2c9f4a63 │ —         │ —                         │  │ 无详情列时留 —（--c-subtle）
│ └───────────────────────────────────────────────────────────────────────────────────────┘  │
│ 3 个会话 · 更新于 13:25:04Z                                        [分页/无]               │ 11px --c-subtle
└────────────────────────────────────────────────────────────────────────────────────────────┘
```

- 「后端可用性」只来自 status 的真实字段：后端段是否存在（该 build 是否编入）+ `android.assets_ok`。缺失时整卡显示 `● 未编入本构建`（`--c-subtle` 文字 + 空心点），不要伪造「不可用」。
- 自动刷新：顶栏右侧一个 `●` 点 + 文案；`●` 在每次成功轮询后从 `--c-ok` 变 `--c-accent-bright` 再回落（`200ms`），让「数据刚更新」可见。
- 轮询失败：页面保留上次数据，顶部插入错误条（§5.14 错误态，区块级），`●` 变 `--c-danger`，文案「已断开 · 重试中」；连续 3 次失败后停轮询，改为手动刷新。
- `reclaimed`（已回收计数）非 0 时，限额卡下方追加一行 `12px` 说明与「查看回收记录」link（点开为对话框，列出 `backend / id / reason / at`，等宽）。

### 7.3 资源管理-检索 `/resources`

```
┌ 顶栏 48px ─────────────────────────────────────────────────────────────────────────────────┐
│ runvm  v0.8.2  │ 系统总览  [资源管理]  MCP调试 │        ● 每 3s  │ 中文|EN │ 退出 │
├────────── 224px ──────────┬───────────────────────────────────────────────────────────────┤
│ ⌕ 检索                    │ padding 24px 32px                                             │
│ ▦ 工具                    │                                                               │
│ ▣ 截图                    │        ┌─ 检索框 720px 居中, 44px 高 ────────────────────┐     │
│ ▤ 文件                    │        │ ⌕  检索资源、工具、截图…                    [/]   │     │
│                           │        └──────────────────────────────────────────────────┘     │
│                           │                    24px                                          │
│                           │  ┌─ 结果区 720px，与检索框左对齐 ────────────────────────┐    │
│                           │  │ 工具 (3)                                    [全部 →] │    │  组头 12/18 500 --c-mute
│                           │  │ ┌────────────────────────────────────────────────────┐│    │  组头 32px 高
│                           │  │ │ runvm_status          JSON  runvm://status         ││    │  结果行 32px；名称等宽 12px
│                           │  │ │ 读取后端、限制与活跃会话                            ││    │  描述 13px --c-body，单行截断
│                           │  │ │ h5_console            JSON  runvm://tools          ││    │  命中高亮 --c-accent-soft
│                           │  │ └────────────────────────────────────────────────────┘│    │
│                           │  │ 截图 (1)                                              │    │
│                           │  │  1f3a9c0e…png   1.2 MiB   2026-11-22T13:20:11Z       │    │
│                           │  │ 文件 (2)                                              │    │
│                           │  │  runvm://files/report.md    1.4 KiB   md            │    │
│                           │  └───────────────────────────────────────────────────────┘    │
│                           │                                                               │
│ 资源根: ~/.runvm/resources│  空检索时：不显示分组，显示「最近访问」6 行 + 分类入口 3 个卡 │
└───────────────────────────┴───────────────────────────────────────────────────────────────┘
```

- 检索为**前端过滤 + 后端 list 的一次拉取**（`/api/resources` 已可拿到三类元信息；工具与截图元信息随页面加载一次性取）。输入即筛选（`120ms` 防抖），不做服务端检索接口——控制台是单机工具，数据量在千级以内。
- 分组顺序固定：**工具 → 截图 → 文件**；某组 0 结果时整组不渲染（不留空标题）。
- 命中高亮：匹配子串包 `<mark>`：`background: --c-accent-soft; color: --c-ink; padding: 0 1px; border-radius: 2px;`
- 键盘：`↓`/`↑` 跨组连续移动（组头跳过），`Enter` 打开（工具 → 工具详情；截图 → 截图页并选中；文件 → 文件页并定位），`Esc` 清空，`/` 聚焦，`Shift+Enter` 在新标签打开原资源（对截图/文件）。
- 结果行右侧显示资源类型 pill 与 URI（等宽 12px，`--c-mute`），URI 过长中间省略（`text-overflow: ellipsis` + `title`）。
- 工具行 URI 语义：`runvm_status` → `runvm://status`（该工具读取的状态资源）；`h5_console` 这类输出是会话 console 缓冲、不落 `runvm://` 资源的工具，URI 写 `runvm://tools`（工具目录来源）。不要给工具编造它并不读取/写入的资源 URI。
- 行尾显示「打开方式」提示：文件类在 hover 时右侧显示 `Enter 打开 · ⌘Enter 新标签`（11px `--c-subtle`）。

### 7.4 资源管理-工具 `/resources/tools`

```
├────────── 224px ──────────┬──────────── 360px ─────────────┬──────────────────────────────┤
│ ⌕ 检索                    │ 工具栏 40px                     │ 详情区 padding 24px          │
│ ▦ 工具  (24)              │ ⌕ 检索工具…           [只读 ▾]  │ runvm_status      [只读]     │ 名称等宽 16px/600
│ ▣ 截图                    │ ┌ 列表 行 28px ───────────────┐ │ runvm://tools 中的只读工具   │ 副标题 12px --c-mute
│ ▤ 文件                    │ │ runvm_status      ● read    │ │ ─────────── hairline ──────  │
│                           │ │ 读取后端、限制与活跃会话     │ │ 描述                         │ 区块标题 14/20 600
│                           │ │ ☑ ios_symbols     ● read    │ │ 读取后端、限制与活跃会话…    │ 13/20 --c-body，最大 640px
│                           │ │   从 Mach-O 导出符号表…     │ │                              │
│                           │ │ ☑ h5_console      ● read    │ │ 参数 (0)                     │
│                           │ │   读取页面 console 缓冲…    │ │ 无参数                        │ 12px --c-mute
│                           │ │ ☐ runvm_upload    ○ write   │ │                              │
│                           │ │   把本机文件上传进资源空间…  │ │ input schema                 │
│                           │ │ ☐ runvm_write     ○ write   │ │ ┌ 深色 JSON 块 ─────────────┐│ 最大高 360px
│                           │ │   写入/覆盖可写资源…        │ │ │ {                          ││
│                           │ └─────────────────────────────┘ │ │   "type": "object",        ││
│                           │ 24 个工具 · 6 只读              │ │   "properties": {}         ││
│                           │                                 │ │ }                          ││
│                           │                                 │ └────────────────────────────┘│
└───────────────────────────┴─────────────────────────────────┴──────────────────────────────┘
```

- 列表行：名称等宽 12px/`--c-ink`，描述单行截断（`--c-mute` 12px）；行高 28px；选中态同 §5.6。
- `☑ / ☐` 表示「是否出现在调试页可调用白名单」（6 个为 ☑），右侧 `● read` / `○ write` pill（read：ok 变体；write：outline 变体）。
- 「参数 (N)」区：以表格列出 `字段 / 类型 / 必填 / 说明`（表头 32px、行 28px、字段与类型等宽），这是给研发与运维读的「摘要」；完整 schema 放下面的深色 JSON 块。
- schema 块头部条带 `input schema` + `复制`；超过 360px 滚动。
- 工具检索只匹配名称与描述；`/` 聚焦；`j/k` 移动，`Enter` 后焦点移到详情区的「复制 schema」按钮。

### 7.5 资源管理-截图 `/resources/screenshots`

```
├────────── 224px ──────────┬────── 280px ────────┬────────────────────────────────────────┤
│ ⌕ 检索                    │ 工具栏 40px         │ 预览工具栏 40px                        │
│ ▦ 工具                    │ ⌕ 检索会话…         │ 1f3a9c0e-6b21-4f7a…png  [打开原图][下载]│
│ ▣ 截图  (1)               │ ┌ 列表 行 28px ────┐│ ┌─────── 预览区 ────────────────────┐  │
│ ▤ 文件                    │ │ ● 1f3a9c0e…png   ││ │  衬底 --c-surface-soft + 棋盘格    │  │
│                           │ │ 1280×720 · 1.2 MB││ │  padding 24px                      │  │
│                           │ │ 13:20:11Z  3 分钟前││ │  ┌──────────────────────────────┐ │  │
│                           │ │ ● 9b7f2c1d…png   ││ │  │  <img 最大 100%×100%，        │ │  │
│                           │ │ 390×844 · 240 KB ││ │  │   object-fit: contain,        │ │  │
│                           │ │ 13:18:40Z  5 分钟前││ │  │   border 1px --c-hairline>    │ │  │
│                           │ └───────────────────┘│ │  └──────────────────────────────┘ │  │
│                           │                      │ └───────────────────────────────────┘  │
│ 资源根: …                 │ 1 张截图             │ 图片信息行 28px: 1280×720 · 1.2 MiB · PNG · 13:20:11Z │
└───────────────────────────┴──────────────────────┴────────────────────────────────────────┘
```

- 列表项两行：第一行文件名（等宽 12px，省略），第二行 `尺寸 · 字节数` + `时间戳 · 相对时间`（11px `--c-subtle`）。
- 「打开原图」/「下载」无后端直出路由（当前 console handler 只暴露 `GET /api/resources/read?uri=…`，二进制以 base64 `blob` 返回）。实现路径：`fetch('/api/resources/read?uri=' + encodeURIComponent(uri))` → `atob(blob)` 转 `Uint8Array` → `new Blob([bytes], { type: mimeType })` → `URL.createObjectURL` → 「打开原图」用 `<a target="_blank" rel="noopener" href=objectURL>` 新标签打开、「下载」用 `<a download="<session>.png" href=objectURL>`。DO-143 可另加一条 `GET /api/resources/raw?uri=…` 直出路由省去 base64 往返，但本规格基线是 object URL 方案（不新增路由、鉴权入口保持单一）。
- 预览要「先占位后加载」：预览框保持 `4:3` 最小高 `240px`，图片 `decoding="async"`，加载中显示骨架（§5.14），失败显示错误态（含后端原始 error）。
- 大图缩放：双击预览在「适应窗口 / 100%」之间切换，100% 时预览区可滚动；工具栏右侧显示当前 `fit / 100%`（12px `--c-mute`）。
- 键盘：`j/k` 切换会话，`o` 打开原图。

### 7.6 资源管理-文件 `/resources/files`

```
├── 224px 目录树 ──┬── 320px 文件列表 ──┬──────────── 1fr 预览/编辑 ──────────────────────┤
│ 工具栏 40px      │ 工具栏 40px         │ 工具栏 40px                                     │
│ ⌕ 路径过滤       │ 面包屑 /docs     ↑ │ docs/report.md  [md] ● 可写   [编辑][删除]      │
│ ┌ 树 行 28px ──┐ │ ┌ 列头 28px ──────┐ │ ┌────── 编辑/预览区 ────────────────────────┐   │
│ │ ▾ runvm://   │ │ │ 名称   大小 修改 │ │ │ 预览态：md 渲染（prose 14/22，最大 720px） │   │
│ │   ▾ docs     │ │ │ ▸ assets/  —   — │ │ │ 编辑态：textarea，等宽 12.5/20，无行号     │   │
│ │     report.md│ │ │   a.png 240K 13:20│ │ │ 等宽行：json / txt / 其他                 │   │
│ │   ▸ assets   │ │ └──────────────────┘ │ └──────────────────────────────────────────┘   │
│ │   notes.txt  │ │ 3 项                │ ┌── 状态条 28px ─────────────────────────────┐ │
│ └──────────────┘ │                     │ │ md · 1.4 KiB · 13:20:11Z · sha256 9f2c…   │ │
│ 资源根: …        │                     │ └───────────────────────────────────────────┘ │
└──────────────────┴─────────────────────┴────────────────────────────────────────────────┘
```

- 目录树：`▸ / ▾` 折叠、`padding-left: 12px × 层`（从 8px 起）、行高 28px、文件名等宽 12px；目录名后可跟 `pill` 显示子项数（12px `--c-mute`）。
- 文件列表：表头 `名称 / 大小 / 修改`（28px 高，12px），行高 28px；目录行带 `▸` 且点击进入；大小右对齐等宽；修改时间只显示 `HH:MM`（今天）或 `MM-DD`（其他），完整值放 `title`。
- 上传入口（三处，同一动作）：
  1. 文件列表工具栏右侧 `上传` primary 按钮（28px）；
  2. 空态里的 ghost 按钮「上传第一个文件」；
  3. 拖拽文件到文件列表区 → 整区出现 `2px dashed --c-accent` 内描边 + 居中提示「松开以上传到 /docs」（13px，`--c-accent`）。
- 上传流程：选择文件 → 弹出 320px 宽的行内确认条（目标路径输入框，预填 `路径/文件名`，等宽）→ 确认后按钮进入加载态 → 成功 toast「已上传 `runvm://files/docs/a.png` · 1.2 MiB · sha256 9f2c…」+ 列表刷新并选中新文件；失败按 §5.14 显示后端原始错误（`413`、`exceeds limit` 等）。
- 多文件：支持多选，逐个上传，右下角 toast 汇总「上传 3 个，成功 2，失败 1」，失败项可展开看每项错误。
- 状态条固定底部 28px：`格式 · 字节数 · 修改时间 · sha256 前 8 位`，全部等宽 11px `--c-subtle`。

### 7.7 MCP 调试 `/debug`

```
┌ 连接条 48px ───────────────────────────────────────────────────────────────────────────────┐
│ ● 已连接 ｜ transport: streamable-http ｜ endpoint: /mcp ｜ protocol: 2025-06-18 │ [重新握手] │
├──── 240px ────┬──────────────────────────────┬─────────────────────────────────────────────┤
│ 方法           │ 参数（请求）                  │ 结果                                         │
│ ┌ 行 28px ───┐│ 标签页: [表单] [JSON]         │ 标签页: [结果] [报文] [schema 检查]          │
│ │ JSON-RPC   ││                              │                                             │
│ │ · initialize││ 方法  tools/call             │ ┌ 深色区 ────────────────────────────────┐  │
│ │ · ping      ││ 工具  [h5_console        ▾]  │ │ {                                      │  │
│ │ · tools/list││                              │ │   "content": [                         │  │
│ │ · tools/call││ 名       类型    必填         │ │     { "type": "text",                 │  │
│ │ · resources/││ session  string  ✓           │ │       "text": "…"                     │  │
│ │ · prompts/  ││ ┌ 值（此工具无参数时置灰）─┐ │ │     }                                  │  │
│ │ 工具（只读）││ │                          │ │ │   ]                                    │  │
│ │ · runvm_st… ││ └──────────────────────────┘ │ │ }                                      │  │
│ │ · h5_console││ [发起调用] ⌘Enter    耗时 —  │ └────────────────────────────────────────┘  │
│ │ 资源/提示词 ││                              │ 状态 200 · 12ms · 420 B                       │
│ ├ 历史 ──────┤│                              │                                              │
│ │ 13:25:04 200││                              │                                              │
│ │ tools/list  ││                              │                                              │
│ └─────────────┘│                              │                                              │
└───────────────┴──────────────────────────────┴─────────────────────────────────────────────┘
```

详细规则见 §9。

---

## 8. 文件浏览器交互细则

### 8.1 按格式打开的映射表（实现按这张表分支，不要靠扩展名猜 MIME）

| 格式 | 判定 | 默认视图 | 可编辑 | 编辑器 | 备注 |
|---|---|---|---|---|---|
| 图片 `png / jpg / jpeg / gif / webp / bmp` | MIME `image/*` 或扩展名 | 图片预览 | 否 | — | 衬底 `--c-surface-soft` + 棋盘格；`object-fit: contain`；显示 `宽×高`（`img.naturalWidth`） |
| JSON | MIME `application/json` 或 `.json` | 树视图（§5.9） | 是 | 同一容器切 textarea（等宽 12.5/20，无行号） | 保存前 `JSON.parse` 校验；失败不提交，错误定位在「第 N 行第 M 列」（原生 `SyntaxError` 消息原样显示） |
| Markdown `.md / .markdown` | 扩展名 | 渲染预览（prose 14/22，最大 720px，标题 20/28→16/24→14/20，代码块走 §5.8，表格走 §5.6） | 是 | 双栏（≥1280）预览 ｜ 编辑；<1280 用 `[预览][编辑]` 标签页 | 渲染用最小子集：标题、段落、行内代码、代码块、列表、引用、链接、表格、分割线；**不引入 markdown 库之外的运行时**（实现阶段可自写 ~150 行或引入 < 20 KB 的库，禁 CDN） |
| 纯文本 `txt / log / yaml / yml / toml / csv / ini / conf / go / js / css / html / sh` | MIME `text/*` 或上列扩展名 | 纯文本（等宽 12.5/20，行号开启，`tab-size: 4`） | 是 | textarea 覆盖层 + 行号列（行号 32px 宽，`--c-mute`，`user-select: none`，与 textarea 同步滚动） | 超过 `2 MiB` 不做语法/行号，直接提示「文件较大，仅支持下载」（`--c-mute` 说明 + 下载按钮） |
| 其他（二进制：pdf 以外的任意二进制、pdf 预览可后续） | 其余全部 | 元信息视图：`大小 / 类型 / 修改时间 / sha256` + 十六进制前 `512 B`（等宽，8 字节一行，偏移量 `00000000` 前缀） | 否 | — | 主操作「下载」；`pdf` 本版不做内嵌预览，按二进制处理（「下载」同图片：`GET /api/resources/read` → base64 `blob` → object URL，无后端直出路由） |

判定顺序写死：**MIME 优先（后端 `mimeType`），缺失时扩展名兜底，都判断不出走「其他」**。不允许在客户端重写 MIME。

### 8.2 编辑态

进入编辑的方式：预览区右上 `编辑` ghost 按钮（28px）或双击文本区，或直接 `e`。

工具栏变体：

```
预览态:  docs/report.md  [md] ● 可写                 [编辑] [下载] [删除]
编辑态:  docs/report.md  [md] ● 未保存                [保存 ⌘S] [放弃 Esc] [格式]
                                                        ↑ primary 28px   ↑ ghost 28px
```

- **未保存标记**：文件名后 pill 由 `● 可写` 变 `● 未保存`（warn 变体）。
- **保存**：`PUT /api/resources/file`，按钮进入加载态（宽不变），状态条右侧出现 `保存中…`（12px `--c-mute`）；成功后 toast `已保存 · 13:25:04Z`（ok 变体）+ 状态条 `sha256` 与 `修改时间` 刷新 + pill 回到 `● 可写`。
- **放弃**：点 `放弃` 或 `Esc` → 立即弹确认（对话框 480px：「放弃未保存的修改？」正文含文件名与「将丢失 N 分钟内的编辑」），确认后回预览态；`Esc` 在已有对话框时先关对话框。
- **保存失败**：对话框（不是 toast）+ 错误态卡片，保留编辑内容，按钮回到可用态，显示后端原始错误（`413 exceeds limit 67108864 bytes`、`path escape` 等）。
- **冲突**：保存前对比 `修改时间`。若服务端 `mtime` 与进入编辑时不同 → 阻止保存，弹出 480px 对话框：
  - 标题「文件已在别处修改」
  - 正文列 `本地（你正在编辑）` / `远端（最后修改 13:25:04Z）` 两个时间与字节数
  - 按钮：`重新载入远端`（danger，放弃本地）/ `仍然覆盖`（danger）/ `取消`（ghost，默认焦点）
  - **默认焦点在「取消」**，覆盖是显式选择。
- 编辑态离开页面（点其他导航）：如果 dirty，浏览器 `beforeunload` 提示；站内跳转用同一个确认对话框。
- `⌘/Ctrl + S` 保存；`Tab` 在 textarea 内插入 `\t`（不是移出焦点），`Esc` 退出编辑。
- 编辑区不自动保存、不做实时协作、不做版本历史（本版范围外）。

### 8.3 只读资源的呈现

| 资源族 | 表现 |
|---|---|
| `runvm://status` | 不进文件浏览器；从总览页以深色 JSON 视图呈现（只读） |
| `runvm://tools` | 进「工具」页（只读）；在文件浏览器顶部固定一行只读入口（不在树里重复） |
| `runvm://screenshot/*` | 进「截图」页（只读） |
| `runvm://pdf/*` | 文件浏览器里不出现；仅在相关会话详情给「下载」（`/api/resources/read` base64 blob → object URL，无后端直出路由） |
| `runvm://files/*` | 唯一可写族；在树中标 `● 可写` |

视觉区分（三处，缺一不可）：
1. `编辑` 按钮不存在（不是 disabled，是根本不渲染）——避免「看起来能点但点不动」。
2. 预览区左侧 `2px` 竖条用 `--c-hairline-strong`，可写资源用 `--c-accent`（`--c-accent` 表示「你可以改这里」）。
3. 状态条最右显示 `只读 / read-only`（outline pill）。

拖拽上传只在可写目录（`runvm://files/*`）生效；拖到只读资源上时显示禁用态提示「此资源只读」。

---

## 9. MCP 调试页布局（对齐 Inspector 的信息分区，只读）

### 9.1 分区映射

| Inspector 的分区 | 本页对应 | 只读处理 |
|---|---|---|
| Connection（传输 / 端点 / 协议版本） | 顶部连接条 48px | 固定展示实际连接信息；`重新握手` 只重发 `initialize` |
| Tools 列表 + 表单 | 左栏「工具（只读）」+ 中栏参数表单 | 只列 6 个白名单工具；写类与状态变更类**列出但不给调用入口**（见 9.4） |
| Request / Response 报文 | 右栏 `报文` 标签 | 请求报文可看可复制，**不可编辑**（参数改在中栏表单） |
| History | 左下「历史」区（默认 160px，可折叠） | 仅本会话内存记录，最多 20 条；不落盘 |
| Schema 检查（portability lint） | 右栏 `schema 检查` 标签 + 顶部摘要 | 只读；基线 `0 error · 3 warning` |
| Resources / Prompts 浏览 | 左栏「资源 / 提示词」分组 | 只 list/get，不提供写 |

### 9.2 尺寸与滚动

```
整页: height calc(100vh - 48px); display: grid;
      grid-template-columns: 240px minmax(320px, 1fr) minmax(320px, 1fr);
      每栏 border-right 1px --c-hairline（末栏无）；每栏独立 overflow: auto
连接条: 48px, border-bottom 1px --c-hairline, padding 0 16px, gap 16px, 12px/18px
左栏: padding 8px; 分组标题 12/18 500 --c-mute (padding 8px, margin-top 12px);
      行 28px; 历史区 border-top 1px --c-hairline, 折叠头 28px
中栏: padding 16px; 参数表单字段间距 12px; 底部操作条 56px (border-top hairline, padding 12px 16px)
右栏: 标签栏 32px; 内容 padding 0（深色区占满宽度，仅上下 12px 留白）
```

### 9.3 左栏：方法 / 工具 / 资源

三组，组头 `12px/18px 500 --c-mute`，行 28px：

1. **JSON-RPC 方法（8 个）**：`initialize` `ping` `tools/list` `tools/call` `resources/list` `resources/read` `prompts/list` `prompts/get`
2. **工具（只读，6 个）**：`runvm_status` `ios_symbols` `ios_imports` `android_symbols` `android_java_stubs` `h5_console`——选中后在组头下缩进一级显示，点选即把中栏方法设为 `tools/call`、工具名填入
3. **资源 / 提示词（只读）**：`resources/list` 返回的 URI 列表 + `prompts/list` 的 3 个提示词，点击把中栏方法设为对应 `read` / `get` 并预填参数

选中态：`background: --c-surface-card; color: --c-ink; font-weight 500`，行尾显示 `●` 表示「刚调用过」。

### 9.4 只读门禁的呈现（安全语义必须可见）

- 左侧另有一组 **「不可调用工具（N 个）」**，**由 `/api/debug/catalog` 的 `callable == false` 在运行时动态生成**，不得写死数量或名称——写死会让新增工具被静默隐藏（见文末 Don't #7）。
- 当前 main 基线（24 个工具 − 6 个白名单 = **18 个不可调用**，实现以实跑 catalog 为准，不照抄数字）：`runvm_close_all` `runvm_upload` `runvm_write` `android_open` `ios_open` `ios_call` `ios_close` `android_hook` `android_java_stub` `android_call` `android_close` `h5_open` `h5_eval` `h5_wait_for` `h5_screenshot` `h5_pdf` `h5_close` `webbrain`。
- 这些行：文字 `--c-subtle`，行尾 `outline` pill `只读页不可调用`，`cursor: not-allowed`，不可选中（`aria-disabled="true"`，`tabindex="-1"`）。
- 鼠标悬停：`title="调试页只读：写类与状态变更类工具不提供调用入口"`。
- 中栏底部操作条：`发起调用` primary，但当前选中项不可调用时整体 `disabled`，并在左侧显示 `该工具不可在此页调用`（12px `--c-danger`）。
- 页面顶部连接条右侧常驻 `只读` pill（ok 变体），**这一点不能在实现时省掉**——它是对使用者的安全声明。

### 9.5 中栏：参数区

两个标签：`表单`（默认）/ `JSON`，切换时不丢已填值（表单 → JSON 序列化；JSON → 表单需解析成功，失败保留 JSON 并标错）。

- **表单**：按选中方法的 schema 生成字段。字段：标签（13px）+ 必填 `*`（`--c-danger`，11px）+ 类型（11px 等宽 `--c-mute`）+ 控件（string → 输入框/textarea；number → 输入框 `type=number`；boolean → 28px 高开关（方形 32×18，选中底 `--c-ink`，无圆角，圆点 `--c-canvas`）；enum → select；object/array → textarea 等宽并校验 JSON）。
- **JSON**：单一 textarea，等宽 12.5/20，最小高 160px，实时校验（`400ms` 防抖），错误在下方 `12px/18px --c-danger`，保存按钮不受影响（调用前再校验）。
- 底部操作条：`[发起调用 ⌘Enter]` primary + `耗时 — / 12ms`（等宽 12px `--c-mute`）+ 右侧 `[重置参数]` ghost。
- 调用中：按钮加载态 + 结果区顶部 `2px` 进度条；不允许并发（按钮禁用）。
- 调用错误（JSON-RPC error）：结果区切到 `报文` 标签并显示完整 error 对象，顶部错误条显示 `code` 与 `message`（后端原文），**不做中文翻译**（见 §11.4）。

### 9.6 右栏：结果 / 报文 / schema 检查

- **结果**：深色 JSON 树（§5.9），默认展开 2 层；`tools/list` 这类大结果默认折叠 `tools` 数组并显示 `… 还有 24 项`。底部状态行 28px：`状态 200 · 12ms · 420 B`（等宽 12px）。
- **报文**：上下两段深色块，各带头部条：`请求`（`--c-dark-elevated`）+ `响应`。请求体可复制不可编辑（`readOnly` 且无光标）。
- **schema 检查**：顶部摘要 `0 error · 3 warning`（`error` 用 `--c-danger`，`warning` 用 `--c-warn`，数字等宽 16px/600）；下方按严重级分组列表：每项 `行 32px`，左 `ERROR`/`WARN` pill，中间工具名（等宽）+ 规则名（`boolean-schema` / `type-union` / `untyped-schema` / `remote-ref`），右侧 `JSON Pointer`（等宽 12px `--c-mute`，省略）。空结果 = 「无 schema 可移植性问题」（ok 变体一行）。
- 三个标签都有计数：`结果`、`报文`、`schema 检查 (3)`。
- 历史区（左栏底部）：每行 24px，`HH:MM:SS` + 方法名（等宽） + 状态点（200 → `--c-ok`，error → `--c-danger`）；点选回填该次调用的方法与参数；最多 20 条，超出滚动；右上 `清空`（12px link）。

---

## 10. 响应式

### 10.1 断点

| 断点 | 宽度 | 布局变化 |
|---|---|---|
| XL | ≥ 1440px | 全量三栏（文件页 224 / 320 / 1fr），检索框 720px，卡片 3 列 |
| L | 1280–1439px | 文件页目录树 208px；工具页列表 320px；检索框 640px；卡片 3 列 |
| M | 1024–1279px | 文件页目录树折叠为工具栏下拉（`路径 ▾`）；工具/截图页维持两栏（列表 280px / 详情 1fr）；卡片 2 列 |
| S | 768–1023px | 侧栏折叠为顶栏下方二级分段控件（40px 高，横向可滚）；文件/工具/截图/调试页改为「列表 → 详情」两级（详情为全宽，带 `← 返回` 28px 高）；卡片 1 列；调试页三栏改为 `[方法 ▾] [参数/结果 标签]` 两区 |
| XS | < 768px | 单栏；顶栏导航改为 `▾` 下拉（48px）；提示条常驻底部：`建议 768px 以上宽度使用 / Best at ≥768px`（12px `--c-mute`，`--c-surface-soft` 底，可关闭） |

### 10.2 折叠规则

- 二级导航（资源管理四项）在 S 及以下折叠为分段控件，`height: 40px`，项 `padding: 0 12px`，横滚不换行（`overflow-x: auto; scrollbar-width: none`）。
- 面包屑在 S 及以下只显示末两级。
- 表格在 S 及以下隐藏次要列（工具页隐藏「参数个数」、文件列表隐藏「修改时间」），保留主列；不出现横向滚动条（除调试页报文区）。
- 调试页报文区允许横滚（等宽长行不折行）。
- 所有触控目标 ≥ `28×28`（S 及以下按钮提到 `32px`）。

### 10.3 最小宽度

`768px` 是设计下限，不做手机端优化（运维场景）。`< 768px` 允许使用但要显示提示条，不允许布局错乱或横向溢出。

---

## 11. 中英文 i18n 设计

### 11.1 语言切换：位置、形态、默认值

**位置**：顶栏右侧，`退出` 左边，距离 `退出` `16px`。

**形态**：两段分段控件（§5.3），总宽 `88px`、高 `28px`：

```
[ 中文 | EN ]     ← 选中段：--c-surface-card 底 + --c-ink 文字
                    未选段：透明底 + --c-mute 文字
```

- 两段标签**在任何语言下都不变**（中文界面显示 `中文 / EN`，英文界面同样显示 `中文 / EN`）。理由：切换语言时，用户需要认出自己**当前**处于哪种语言，把当前语言翻译成另一种语言会造成「EN」变「英文」这种自我否定的混乱。
- 点击即时切换，不刷新页面；`html[lang]` 同步更新。

**默认语言判定链**（按顺序，先命中先用）：

1. URL 参数 `?lang=zh-CN` / `?lang=en` → 命中即写入持久化并跳掉参数（`history.replaceState`）
2. 持久化偏好 `localStorage["runvm.lang"]`（值 `zh-CN` 或 `en`）
3. Cookie `runvm_lang`（与 localStorage 同写，用于首屏 `<html lang>` 预置，避免闪动）
4. `navigator.languages` 中第一个 `zh*` → `zh-CN`；`en*` → `en`
5. 兜底 `zh-CN`（仓库文档是中文，主要使用者是中文用户；英文只是可选界面）

**首屏防闪动**：`index.html` 里放一段内联脚本（< 200 字节），在 `<head>` 内读 cookie，立即 `document.documentElement.lang = ...` 与 `document.documentElement.dataset.lang = ...`；文案表仍是 JS 加载后填，但首屏骨架先按语言的 `line-height` 渲染，避免布局跳动。

**持久化写入**：切换时同时写 `localStorage` 与 `cookie`（`runvm_lang=...; path=/; max-age=31536000; SameSite=Lax`，非 HttpOnly）。

### 11.2 文案 token 体系

**所有界面文案都是 token，模板里不允许出现裸字符串**（含列名、占位符、空态、错误提示、按钮、aria-label、title 属性）。

命名规范：`<域>.<位置>.<键>`，全小写、点分隔、camelCase 用下划线分隔的多词键按下述分类：

| 域 | 覆盖 | 例子 |
|---|---|---|
| `nav.*` | 顶层与二级导航 | `nav.overview` / `nav.resources` / `nav.resources.tools` / `nav.resources.files` / `nav.debug` |
| `page.*` | 页面标题与副标题 | `page.overview.title` / `page.overview.subtitle` |
| `action.*` | 按钮与操作 | `action.save` / `action.discard` / `action.upload` / `action.retry` / `action.reload_remote` |
| `field.*` | 表单标签与占位符 | `field.username` / `field.target_path` / `field.search_tools` |
| `table.*` | 表头 | `table.col.backend` / `table.col.session` / `table.col.size` / `table.col.modified` |
| `state.*` | 状态与进度 | `state.loading` / `state.saving` / `state.saved` / `state.readonly` / `state.dirty` |
| `empty.*` | 空态标题与说明 | `empty.files.title` / `empty.files.desc` / `empty.search.title` |
| `error.*` | 错误标题与外层说明 | `error.load_failed` / `error.save_failed` / `error.network` / `error.unauthorized` |
| `unit.*` | 单位与量词 | `unit.bytes` / `unit.ms` / `unit.sessions`（用于「3 个会话」/「3 sessions」） |
| `meta.*` | 元信息标签 | `meta.version` / `meta.idle_timeout` / `meta.sha256` / `meta.writable` |
| `a11y.*` | 无障碍标签 | `a11y.lang_toggle` / `a11y.copy` / `a11y.close` |
| `debug.*` | 调试页 | `debug.methods` / `debug.tools_readonly` / `debug.schema_check` / `debug.history` |

**占位符**：`{name}` 单花括号插值，只支持字符串替换，不引 i18n 库、不做 ICU 复数规则：

```json
{ "table.count": "{n} 项" }
```
```json
{ "table.count.one": "{n} item", "table.count.other": "{n} items" }
```

英文需要单复数时按 `.one` / `.other` 两份键，调用点传 `n`，取键时判断 `n === 1`。中文只有一份键。

**存储与加载**：`web/locales/zh-CN.json` 与 `web/locales/en-US.json`，构建期 inline 进 JS（不额外请求，保证离线单二进制）。两文件的键集合必须完全一致 —— 加一条 CI/构建期校验（脚本比对键差集，缺键直接 fail），否则英文界面早晚会漏文案。

**取值 API**：`t("action.save")`、`t("table.count", { n: 3 })`；`t` 缺失时返回键名本身并在 `console.warn` 打出 —— 缺文案要在开发时立刻可见，不要静默回退成空。

**渲染**：静态骨架用 `data-i18n="nav.overview"` 属性 + 一次 `applyI18n(root)` 扫描替换，动态节点直接 `t()`。切换语言 = 重新执行一次扫描 + 重渲染当前视图。

### 11.3 不翻译清单（硬约束，研发据此避免过度本地化）

以下内容**永远不翻译、不做大小写变换、不加空格**：

1. 工具名：`runvm_status` `h5_console` `ios_symbols` …（24 个）
2. 参数名与 schema 字段：`backends` `session` `target` `content` `base64` …
3. 资源 URI：`runvm://files/…` `runvm://screenshot/…`
4. JSON 键与 JSON 值（含布尔 `true/false`、`null`）
5. 后端名：`android` `ios` `h5`、引擎字段 `in_use` `idle` `created` `pool`
6. 会话 ID、文件路径、文件名、sha256、版本号、go 版本、`os/arch`
7. HTTP 方法/状态码：`GET` `POST` `PUT` `200` `401` `413`
8. JSON-RPC 方法与错误 `code`/`message`：`tools/call` `-32602` `Invalid params`
9. 后端原始错误文本：`path escapes resource root` `exceeds limit 67108864 bytes`
10. 快捷键字符：`⌘` `Ctrl` `Esc`（`Ctrl` 在 Windows/Linux 下保持英文，不写「控制」）
11. 单位符号：`KiB` `MiB` `ms` `B`；时间戳 ISO 格式
12. 品牌：`runvm`、`MCP`、`JSON`、`PNG`、`Markdown`

**唯一的「翻译」是外层包裹**：错误态标题 `加载失败 / Load failed` 可以翻，但紧随其后的原始 error 必须原样一行等宽显示。这一条比翻译更重要 —— 运维看到的报错必须能和后端日志、源码对上。

### 11.4 排版对两种语言的处理

| 项 | 中文 | 英文 |
|---|---|---|
| body 字号/行高 | `13px/20px` | `13px/20px`（同值，切语言不跳） |
| prose | `14px/22px` | `14px/22px` |
| 页面标题 | `20px/28px`，`letter-spacing: 0` | `20px/28px`，`letter-spacing: -0.01em` |
| section 标题 | `14px/20px` 600 | `14px/20px` 600，`letter-spacing: -0.005em` |
| 段落最大宽 | `720px`（中文舒适行长约 38–42 字） | `680px`（英文约 75–85 字符） |
| 词间距 | 不处理 | `word-spacing: 0`，依赖字体原生 |
| 换行 | `word-break: normal; overflow-wrap: anywhere`（仅 URI/长串用 `break-all`） | 同 |

**中文下的等宽落法**（复述 §3.5 的结论，因为这里最容易做错）：

- 等宽位置的 font-family 顺序：`var(--font-mono)` 在前，`var(--font-mono-cjk)` 作为中文回退；实际写成一个栈：`ui-monospace, "SF Mono", Menlo, Consolas, "Sarasa Mono SC", "Noto Sans Mono CJK SC", monospace`。
- 允许中文不齐；**绝不靠等宽对齐列**（列宽一律 CSS 控制）。
- 等宽区的中文行高与英文同为 `20px`，`line-height` 显式设置，不用 `normal`。
- 中文出现在等宽区（如 `报告-2026.md`）时不缩小字号、不加字间距。

### 11.5 文案长度容忍度

英文通常比中文长 30–60%，必须按 1.5–1.6 倍留余量：

| 组件 | 必须容纳 | 溢出策略 |
|---|---|---|
| 按钮 | 1.6× | 不换行（`white-space: nowrap`），宽度随内容；工具栏内最小宽 `64px`，最大不超过容器 40% |
| 导航项（顶栏） | 1.5× | 不截断；顶栏中部空间不足时把品牌版本 pill 隐藏（S 断点） |
| 侧栏项 | 1.5× | 超长截断 `text-overflow: ellipsis` + `title` 全量；侧栏宽度固定 224px 不随语言变（宽度跳变比截断更差） |
| 表格头 | 1.6× | 优先换行（`white-space: normal`，最多 2 行，行高 16px），仍超则截断 + `title` |
| 表单标签 | 1.6× | 换行；标签与控件同一列布局（标签在上） |
| pill / 徽标 | 1.5× | 不截断（短词），宽度自适应 |
| 空态说明 | 2× | 换行，最大宽 320px（中文）/ 360px（英文） |
| toast | 2× | 换行；宽 320px 固定，最长 3 行后截断 |
| 对话框标题 | 1.5× | 换行，最多 2 行 |
| tab 标签 | 1.5× | 不截断（标签 ≤ 4 个），超出容器则横滚 |

**必须能容纳 1.5 倍**：按钮、导航项、表头、表单标签、pill、tab。
**允许截断**：侧栏项、面包屑、URI、文件名、toast 正文、表格单元格内的描述。
**绝不允许截断**：错误信息正文、会话 ID（宁可横滚）、sha256（分段展示）。

### 11.6 数字 / 日期 / 单位

| 内容 | 中文 | 英文 | 说明 |
|---|---|---|---|
| 时间戳（精确值） | `2026-11-22T13:25:04Z` | 同 | ISO 8601 UTC，等宽，不本地化（跨语言对日志） |
| 相对时间 | `3 分钟前` | `3 min ago` | `Intl.RelativeTimeFormat(lang, { numeric: "auto" })`；< 60s 显示 `just now` / `刚刚` |
| 表格内修改时间 | 今天 `13:25`；其他 `11-22 13:25` | 同（24 小时制） | 不因语言改成 12 小时制 —— 运维按 24h 读日志 |
| 字节数 | `1.4 MiB` / `240 KiB` / `12 B` | 同 | 二进制单位 `KiB/MiB/GiB`，单位不翻译；换算 `< 10` 保留 1 位小数，`≥ 10` 取整；`0` 显示 `0 B` |
| 字节数（原始） | `1468006 B` 等宽于 `title` | 同 | 精确值放 `title` |
| 会话数 | `3 个会话` | `3 sessions` | 走 `unit.sessions` + 单复数键 |
| 计数（工具数等） | `24 个工具` | `24 tools` | 同上 |
| 毫秒 | `12ms` | 同 | 不加空格，等宽 |
| 百分比（无） | — | — | 本版无百分比指标 |
| 日期选择/排序 | 按 UTC 排序 | 同 | 排序依据原始值，不依据显示文本 |

`Intl` 用法限定：`Intl.RelativeTimeFormat` 与 `Intl.NumberFormat`（仅用于 `≥ 1000` 的计数分组）。**不用 `Intl.DateTimeFormat`** —— 时间戳必须语言无关。

---

## 12. 落地说明

### 12.1 CSS 变量总表（`web/src/styles/tokens.css`，全站唯一定义处）

```css
:root {
  /* color — chrome */
  --c-canvas: #fdfcfc;
  --c-surface-soft: #f8f7f7;
  --c-surface-card: #f1eeee;
  --c-ink: #201d1d;
  --c-ink-deep: #0f0000;
  --c-body: #424245;
  --c-mute: #646262;
  --c-subtle: #8a8787;
  --c-disabled: #b3afae;
  --c-hairline: rgba(15, 0, 0, 0.12);
  --c-hairline-strong: #d5d0d0;

  /* color — accent & semantic */
  --c-accent: #0b57d0;
  --c-accent-hover: #094ab3;
  --c-accent-active: #073b8f;
  --c-accent-soft: #e8eefb;
  --c-accent-bright: #007aff;
  --c-ok: #1f7a3d;
  --c-warn: #8a5a00;
  --c-danger: #b3261e;
  --c-ok-soft: #e6f2e9;
  --c-warn-soft: #fdf3e0;
  --c-danger-soft: #fbeceb;

  /* color — dark surfaces */
  --c-dark: #201d1d;
  --c-dark-elevated: #302c2c;
  --c-dark-line: rgba(253, 252, 252, 0.12);
  --c-on-dark: #fdfcfc;
  --c-on-dark-mute: #9a9898;
  --c-syn-string: #9ccc9c;
  --c-syn-number: #ffb86b;
  --c-ok-bright: #30d158;
  --c-warn-bright: #ff9f0a;
  --c-danger-bright: #ff3b30;
  --c-scrim: rgba(32, 29, 29, 0.32);

  /* type */
  --font-ui: system-ui, -apple-system, "Segoe UI", "PingFang SC", "Hiragino Sans GB",
             "Noto Sans SC", "Microsoft YaHei", sans-serif;
  --font-mono: ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas,
               "Cascadia Mono", "Sarasa Mono SC", "Noto Sans Mono CJK SC", monospace;
  --fs-title: 20px;   --lh-title: 28px;
  --fs-section: 14px; --lh-section: 20px;
  --fs-body: 13px;    --lh-body: 20px;
  --fs-prose: 14px;   --lh-prose: 22px;
  --fs-meta: 12px;    --lh-meta: 18px;
  --fs-micro: 11px;   --lh-micro: 16px;
  --fs-mono: 12.5px;  --lh-mono: 20px;
  --fs-mono-sm: 12px; --lh-mono-sm: 18px;

  /* space */
  --s-0: 0; --s-1: 4px; --s-2: 8px; --s-3: 12px; --s-4: 16px;
  --s-5: 20px; --s-6: 24px; --s-8: 32px; --s-10: 40px; --s-12: 48px;

  /* radius */
  --r-none: 0; --r-sm: 4px; --r-md: 6px; --r-full: 999px;

  /* elevation — 全站无阴影 */
  --shadow-none: none;
}
```

`§2–§4` 里每个 token 与这里一一对应；实现阶段**不允许在组件里直接写色值/字号**，只能用变量（评审时 grep 检查 `#` 十六进制出现在 `tokens.css` 之外即为问题）。

### 12.2 组件清单（复用 vs 页面级）

**可复用组件（`web/src/components/`，一个文件一个组件）**

| 组件 | 关键 props / 状态 | 用在哪 |
|---|---|---|
| `Button` | `variant: primary\|ghost\|danger\|link\|icon`、`loading`、`disabled`、`size: sm\|md` | 全站 |
| `Segmented` | `items`、`value`、`onChange` | 语言切换、窄屏二级导航、结果标签 |
| `Tabs` | `items`、`value`、`count` | 结果区、报文区、编辑/预览 |
| `Input` / `Textarea` / `Select` | `value`、`error`、`placeholder`、`mono` | 全站表单 |
| `SearchField` | `size: sm\|lg`、`value`、`onInput` | 各页检索 |
| `DataTable` | `columns[{key,label,align,width,mono}]`、`rows`、`selectedKey`、`onSelect`、`empty`、`loading`、`dense` | 总览会话表、工具参数表、文件列表 |
| `Card` | `title`、`meta`、`actions` | 总览、限额、空态容器 |
| `Pill` | `variant: neutral\|ok\|warn\|danger\|outline`、`mono`、`dot` | 全站 |
| `Kbd` | `keys[]` | 快捷键提示 |
| `CodeBlock` | `code`、`lang`、`title`、`copyable`、`maxHeight` | schema、报文、二进制元信息 |
| `JsonTree` | `data`、`defaultExpandDepth`、`highlight`、`onCopyPath` | JSON 预览、调试结果 |
| `EmptyState` | `icon`、`title`、`desc`、`action` | 全站 |
| `Skeleton` | `rows`、`variant: table\|block\|text` | 全站加载态 |
| `ErrorState` | `title`、`message`、`detail`、`onRetry` | 全站错误态 |
| `Toast` | `variant`、`title`、`detail`、`timeout` | 保存/上传/复制反馈 |
| `Dialog` | `title`、`body`、`actions`、`danger` | 覆盖确认、冲突、删除 |
| `SidebarNav` | `items[{key,label,icon,count}]` | 资源管理 |
| `Breadcrumb` | `segments`、`current` | 文件页 |
| `StatusDot` | `state: live\|ok\|warn\|danger\|off` | 顶栏、后端卡、历史 |

**页面级（`web/src/pages/`，不抽成通用组件）**

| 页面 | 组成 |
|---|---|
| `Login` | 表单列 + 错误文本 + 版本行 |
| `Overview` | 3 后端卡 + 3 限额卡 + 会话 DataTable + 轮询控制 |
| `ResourceSearch` | 大检索框 + 分组结果 + 最近访问 |
| `ResourceTools` | 列表 + 详情（描述 / 参数表 / schema CodeBlock） |
| `ResourceScreenshots` | 列表 + 预览框 + 缩放控制 |
| `ResourceFiles` | 目录树 + 文件列表 + 编辑器容器（按格式分支）+ 拖拽上传 |
| `McpDebug` | 连接条 + 方法列表 + 历史 + 参数表单 + 结果三标签 + schema 检查 |

### 12.3 建议文件结构（供 DO-143 消费，不强制）

```
web/
├── index.html              <html lang> 由内联脚本预置
├── locales/{zh-CN,en-US}.json
├── src/
│   ├── styles/tokens.css   §12.1 唯一的变量定义处
│   ├── styles/base.css     重置、字体、focus-visible、滚动条
│   ├── i18n.ts             t() / applyI18n() / 语言判定与持久化（~80 行）
│   ├── format.ts           fmtBytes / fmtTime / fmtRelative / fmtCount（§11.6）
│   ├── api.ts              所有 /api/* 调用与错误归一化（含 401 → 登录页）
│   ├── components/         §12.2 上表
│   └── pages/              §12.2 下表
```

### 12.4 现状迁移对照（视觉改动的具体对象）

| 现状 | 新值 | 备注 |
|---|---|---|
| `--bg: #0f1216` 深色底 | `--c-canvas: #fdfcfc` | 整站明暗翻转 |
| `--accent: #4da3ff` | `--c-accent: #0b57d0` | 蓝色降饱和、提对比度到 AA |
| `border-radius: 10px`（卡片） | `0`（卡片）/ `4px`（控件） | 圆角只留给可点元素 |
| `border-radius: 6px`（控件） | `4px` | |
| 4 个平级 tab（状态/资源/上传/MCP 调试） | 3 个顶层 + 资源管理 4 个二级 | 上传并入文件页（§6.2） |
| `font: 14px/1.5` 全站无衬线 | 13px + 值用等宽 | §1.5 |
| 无 i18n，中文硬编码 | token 体系 + 语言切换 | §11 |
| `.card { padding: 24px }` | `16px` | 密度提升 |
| 表格无列宽/行高约束 | 表头 32px / 行 32px / 明确列宽 | §5.6 |

---

## 13. 验收自检表（对照本单验收标准）

| # | 验收点 | 本文档对应 | 可对照的证据 |
|---|---|---|---|
| 1 | 视觉参照 OpenCode | §1.1 继承表（色值/圆角/hairline/无阴影）+ §2–§5 全部 token | 每个色值可回溯到 opencode.ai 模板或明确标注的分歧 |
| 2 | 导航三段 + 资源管理四页 | §6.1 结构树、§6.2 功能边界、§7.2–§7.6 线框 | 每页一条路径、一张线框 |
| 3 | 文件浏览器按格式打开 + 图片/json/md/txt 预览与编辑 | §8.1 映射表、§8.2 编辑态、§8.3 只读呈现 | 5 类格式的分支与控件写死 |
| 4 | 每个 token / 组件有具体数值 | §2–§5 | 无「更现代/更简洁」类描述 |
| 5 | 线框细到能写代码 | §7.1–§7.7 | 每个区块标了宽高、区块层级、操作点 |
| 6 | 离线可用 | §3.1 字体栈、§5.8 图标策略、§11.2 locale 内联 | 零 CDN、零外部图片、零在线图标库 |
| 7 | 中英文：切换位置/形态/默认判定 | §11.1 | 位置、尺寸、5 步判定链 |
| 8 | 中英文：文案 token 体系 | §11.2 | 12 个域 + 占位符规则 + 缺键校验 |
| 9 | 中英文：不翻译清单 | §11.3 | 12 条硬约束 |
| 10 | 中英文：混排排版与等宽在中文下的落法 | §3.4、§3.5、§11.4 | 行高/字重/回退栈/禁对齐规则 |
| 11 | 中英文：长度容忍与截断策略 | §11.5 | 三档：必须容纳 / 允许截断 / 绝不允许 |
| 12 | 中英文：数字日期单位 | §11.6 | 每条给两语言的具体形态 |
| 13 | 响应式 | §10 | 5 个断点 + 折叠规则 + 最小宽度 |
| 14 | 落地映射 | §12 | CSS 变量表 + 组件清单 + 文件结构 + 迁移对照 |
| 15 | MCP 调试页对齐 Inspector 且只读 | §9 | 6 个分区映射 + 写类工具的禁用呈现 |
| 16 | 数据不编造 | §6.2、§7.2 | 所有字段来自 `GET /api/status` 实际返回结构；未落地能力明确标为不做（PDF 内嵌预览、版本历史、实时协作） |

---

**Do's & Don'ts（实现时最容易跑偏的 8 条）**

1. **Do** 用 1px hairline 分区；**Don't** 用色块分区或 `box-shadow` 造层级。
2. **Do** 值用等宽；**Don't** 全站等宽，也别给中文标签加字间距。
3. **Do** 深色只给只读机器产物；**Don't** 把可编辑区做成深色。
4. **Do** 强调蓝只用于链接/焦点/候选/生效；**Don't** 用它做卡片描边或大背景。
5. **Do** 显示后端原始错误；**Don't** 用「请稍后重试」覆盖它。
6. **Do** 让按钮宽度随文案生长（容纳 1.5×）；**Don't** 给按钮写死像素宽。
7. **Do** 把写类工具在调试页显示为禁用并标注「只读页不可调用」；**Don't** 静默隐藏（隐藏会让人以为是能力缺失）。
8. **Do** 语言切换用两段自查标签；**Don't** 把「中文」翻译成「Chinese」。
