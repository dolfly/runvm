---
name: webbrain-automation
description: >-
  Drive real web pages as a lightweight automation tool through runvm's MCP
  server (h5_open + webbrain + h5_* tools). Use when a task needs the agent to
  open a page, read it as an accessibility tree, and click/type/scroll/check by
  stable ref_ids — replacing manual clicking without a browser extension.
  Trigger words: 浏览器自动化, 页面操作, webbrain, 多步网页任务, fill this form,
  click that button, 打开页面并, automate this page.
---

# WebBrain automation via runvm MCP

runvm exposes WebBrain's own page-control scripts (accessibility tree + DOM
actions) as MCP tools. This skill tells the agent how to orchestrate them for
multi-step browser automation. The MCP server must be reachable with the
`webbrain` and `h5_*` tools (runvm ≥ the build with the `webbrain` tool; assets
at `assets/h5/webbrain/`).

## Tool surface

| tool | purpose |
|---|---|
| `h5_open` | open a URL, `{"webbrain": true}` auto-injects the page scripts |
| `webbrain` | one command, many actions (see table) |
| `h5_wait_for` | poll until a selector/expression is truthy |
| `h5_eval` | arbitrary JS (Promise awaited; no top-level `await`) |
| `h5_screenshot` / `h5_pdf` | capture evidence (needs `visible: true` on Linux/Windows) |
| `h5_console` | page console errors |
| `h5_close` | close the session |

`webbrain` actions: `page` (title/desc/forms/links), `tree` (accessibility
tree, flat text with stable `ref_id`s), `interactive` (control list:
role/name/ref), `snapshot` (page + compact tree, first look), `click`
(ref_id/text/selector/coords), `type` (ref_id/text/clear), `press`
(Escape/Tab/Enter/arrows), `scroll` (ref_id or x/y), `check` (ref_id/checked).

## The loop: perceive → plan → act → verify

1. **Open**: `h5_open {"target": <url>, "webbrain": true, "visible": false}`.
   Keep the session id. If the page needs time, `h5_wait_for` a real selector —
   do not act on a document whose body is still empty.
2. **Perceive**: `webbrain snapshot` (or `tree` with `filter:"interactive"`).
   Use small `max_chars` reads; re-read when the page changes.
3. **Plan**: enumerate concrete steps, each addressed by a `ref_id` from the
   latest read. Never reuse a `ref_id` across a navigation or a big DOM change.
4. **Act** one step at a time: `click`/`type`/`press`/`scroll`/`check`. After an
   action that changes the page, re-read before the next action.
5. **Verify**: confirm the outcome with a fresh read (`webbrain page` / `tree`
   / `h5_eval`) before declaring success. If a `ref_id` is stale or missing,
   re-read the tree and retry with the new id.
6. **Report**: action + ref_id + result per step, what was verified, and whether
   the task is complete. Attach a screenshot when evidence matters.
7. **Close**: `h5_close` when done.

## Rules

- Prefer `ref_id` addressing; fall back to `text`/`selector`/coordinates only
  when no ref fits.
- Read small, often: cap `max_chars` (~2000–4000) so each step stays light.
- After navigation (full page load) the injected globals are gone; the
  `webbrain` tool re-injects automatically, but the tree must be re-read — old
  ref_ids are invalid.
- `press` supports only Escape, Tab, Enter, the four arrows and `;`.
- The MCP prompt `webbrain-automate` renders this same plan as an instantiable
  workflow — prefer `prompts/get webbrain-automate {task, url}` when the client
  supports MCP prompts, then follow it.
- Login walls, CAPTCHAs and bot checks may block actions; report the blocker
  instead of forcing through.

## Example session (multi-step)

Task: open xiaohongshu, read the discover feed, scroll once, click the first
note, summarize it.

1. `h5_open {"target":"https://www.xiaohongshu.com","webbrain":true}` → h5-1
2. `h5_wait_for {"session":"h5-1","selector":".feeds-container, main"}` or poll `h5_eval` for `document.body.innerText.length > 500`
3. `webbrain {"session":"h5-1","action":"snapshot","max_chars":1500}`
4. `webbrain {"session":"h5-1","action":"tree","filter":"interactive","max_depth":10,"max_chars":2000}` → note ref_ids
5. `webbrain {"session":"h5-1","action":"scroll","y":800}` → re-read tree
6. `webbrain {"session":"h5-1","action":"click","ref_id":"ref_..."}` (first note link)
7. `h5_wait_for` / sleep, then `webbrain {"session":"h5-1","action":"page"}` + `tree` → summarize the note
8. `h5_close {"session":"h5-1"}`
