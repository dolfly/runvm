# WebBrain content scripts (verbatim, GPL-3.0-or-later)

These files are the page-side scripts WebBrain injects into every tab,
copied verbatim from [webbrain-one/webbrain](https://github.com/webbrain-one/webbrain)
v49.1.2 (commit `25244231fe0825467217c19f12c158b7123ca323`, 2026-11-18) to give
runvm's H5 backend the same page-perception and page-control capabilities:

| file | source (`src/chrome/src/content/`) | purpose |
|---|---|---|
| `shim.js` | runvm-authored (MIT) | stubs `chrome.runtime`, captures the `onMessage` handlers the scripts register, and exposes `window.__webbrainRun(action, params)` |
| `accessibility-tree.js` | `accessibility-tree.js` | flat indented accessibility tree with stable `ref_id`s (`__generateAccessibilityTree`, `__wb_ax_lookup`, ...) |
| `content.js` | `content.js` | page reading + DOM actions: click/click_ax, type, press_keys, scroll, set_checked, get_page_info, get_interactive_elements, wait_for_stable, extract_data, ... |

## License

`accessibility-tree.js` and `content.js` are
[GPL-3.0-or-later](./LICENSE) (WebBrain's own license; see the copied
`LICENSE` file here). `shim.js` is MIT. The scripts are loaded at runtime as
asset files (the same pattern runvm uses for the bionic sysroot) and are not
compiled into the runvm binary.

## How it is used

- `h5_open` accepts `"webbrain": true` and evaluates the three files in the
  page after it loads (shim first, then accessibility-tree.js, then content.js).
- The `webbrain` MCP tool then dispatches through `window.__webbrainRun`:
  `tree` / `interactive` / `page` / `snapshot` for perception,
  `click` / `type` / `press` / `scroll` / `check` for control.

If a session's page navigates away (full reload), the globals are lost; the
`webbrain` tool re-injects automatically before the next call.
