// Shared UI primitives: pill, JSON tree, code block, empty/error states, toast
// and dialog. Kept tiny and dependency-free.

import { el, clear } from "./dom";
import { t } from "./i18n";

export function pill(
  variant: "neutral" | "ok" | "warn" | "danger" | "outline",
  text: string,
  opts: { mono?: boolean; dot?: string } = {},
): HTMLElement {
  const p = el("span", { class: `pill ${variant}${opts.mono ? " mono" : ""}` });
  if (opts.dot) p.appendChild(el("span", { class: `dot ${opts.dot}` }));
  p.appendChild(document.createTextNode(text));
  return p;
}

export function jsonTree(data: unknown, defaultDepth = 2): HTMLElement {
  const root = el("div", { class: "json-tree" });
  root.appendChild(nodeBlock(undefined, data, 0, defaultDepth));
  return root;
}

function isContainer(v: unknown): v is Record<string, unknown> | unknown[] {
  return v !== null && typeof v === "object";
}

// nodeBlock renders one key/value subtree as a single element: a header line,
// then (when expanded) nested children and a close-bracket line. Primitive
// values are a single line.
function nodeBlock(key: string | undefined, value: unknown, depth: number, defaultDepth: number): HTMLElement {
  if (!isContainer(value)) {
    const line = el("div", { class: "jline", style: `padding-left:${depth * 16}px` });
    if (key !== undefined) {
      line.appendChild(el("span", { class: "jk" }, key));
      line.appendChild(el("span", { class: "jp" }, ": "));
    }
    line.appendChild(scalarSpan(value));
    return line;
  }

  const isArr = Array.isArray(value);
  const entries: [string, unknown][] = isArr
    ? (value as unknown[]).map((v, i) => [String(i), v])
    : Object.entries(value as Record<string, unknown>);
  const collapsed = depth >= defaultDepth && entries.length > 0;

  const wrap = el("div", { class: "jnode" });
  const header = el("div", { class: "jline", style: `padding-left:${depth * 16}px` });
  if (key !== undefined) {
    header.appendChild(el("span", { class: "jk" }, key));
    header.appendChild(el("span", { class: "jp" }, ": "));
  }
  const arrow = el("span", { class: "jarrow" }, entries.length ? (collapsed ? "▸" : "▾") : " ");
  header.appendChild(arrow);
  header.appendChild(el("span", { class: "jp" }, isArr ? "[" : "{"));
  if (collapsed) {
    header.appendChild(el("span", { class: "jsumb" }, `… ${entries.length} ${t("unit.items.other", { n: entries.length })}`));
    header.appendChild(el("span", { class: "jp" }, isArr ? "]" : "}"));
    arrow.style.cursor = "pointer";
    header.style.cursor = "pointer";
    header.addEventListener("click", () => {
      const expanded = nodeBlock(key, value, depth, 0);
      wrap.replaceWith(expanded);
    });
    wrap.appendChild(header);
    return wrap;
  }
  wrap.appendChild(header);
  if (entries.length === 0) {
    header.appendChild(el("span", { class: "jp" }, isArr ? "]" : "}"));
    return wrap;
  }
  arrow.style.cursor = "pointer";
  let open = true;
  const children = el("div", { class: "jchildren" });
  for (const [k, v] of entries) children.appendChild(nodeBlock(k, v, depth + 1, defaultDepth));
  const closeLine = el("div", { class: "jline", style: `padding-left:${depth * 16}px` }, el("span", { class: "jp" }, isArr ? "]" : "}"));
  wrap.appendChild(children);
  wrap.appendChild(closeLine);
  header.addEventListener("click", () => {
    open = !open;
    arrow.textContent = open ? "▾" : "▸";
    children.style.display = open ? "" : "none";
    closeLine.style.display = open ? "" : "none";
  });
  return wrap;
}

function scalarSpan(value: unknown): HTMLElement {
  if (value === null) return el("span", { class: "jnull" }, "null");
  const t = typeof value;
  if (t === "string") return el("span", { class: "jstr" }, JSON.stringify(value));
  if (t === "number" || t === "boolean") return el("span", { class: "jnum" }, String(value));
  return el("span", { class: "jnum" }, String(value));
}

export function codeBlock(code: string, title?: string, copyable = false): HTMLElement {
  const block = el("div", { class: "codeblock" });
  if (title || copyable) {
    const head = el("div", { class: "head" });
    if (title) head.appendChild(el("span", {}, title));
    if (copyable) {
      const btn = el("button", { class: "btn icon sm", style: "color:var(--c-on-dark-mute)" }, "⧉");
      btn.title = t("a11y.copy");
      btn.addEventListener("click", async () => {
        await navigator.clipboard.writeText(code);
        btn.textContent = "✓";
        setTimeout(() => (btn.textContent = "⧉"), 1200);
      });
      head.appendChild(btn);
    }
    block.appendChild(head);
  }
  block.appendChild(el("pre", {}, code));
  return block;
}

export function emptyState(icon: string, title: string, desc: string, action?: HTMLElement): HTMLElement {
  const s = el("div", { class: "state" });
  s.appendChild(el("div", { class: "ic" }, icon));
  s.appendChild(el("div", { class: "title" }, title));
  if (desc) s.appendChild(el("div", { class: "desc" }, desc));
  if (action) s.appendChild(el("div", { class: "act" }, action));
  return s;
}

export function errorState(title: string, detail: string, onRetry?: () => void): HTMLElement {
  const box = el("div", { class: "error-box" });
  const body = el("div", { class: "spacer" });
  body.appendChild(el("div", { class: "title" }, title));
  if (detail) body.appendChild(el("div", { class: "tech" }, detail));
  box.appendChild(body);
  if (onRetry) box.appendChild(el("button", { class: "btn ghost sm", onclick: onRetry }, t("action.retry")));
  return box;
}

// --- toast ---

let toastHost: HTMLElement | null = null;
function host(): HTMLElement {
  if (!toastHost) {
    toastHost = el("div", { class: "toast-host" });
    document.body.appendChild(toastHost);
  }
  return toastHost;
}

export function toast(variant: "ok" | "warn" | "danger", title: string, detail?: string, timeout = 5000): void {
  const node = el("div", { class: `toast ${variant}` });
  node.appendChild(el("div", { class: "t" }, title));
  if (detail) node.appendChild(el("div", { class: "d" }, detail));
  host().appendChild(node);
  let hovered = false;
  node.addEventListener("mouseenter", () => (hovered = true));
  node.addEventListener("mouseleave", () => (hovered = false));
  setTimeout(() => {
    if (!hovered) node.remove();
  }, timeout);
}

// --- dialog ---

export function dialog(title: string, body: HTMLElement | string, actions: HTMLElement[]): { close: () => void } {
  const scrim = el("div", { class: "scrim" });
  const dlg = el("div", { class: "dialog" });
  dlg.appendChild(el("h2", {}, title));
  const b = el("div", { class: "body" });
  if (typeof body === "string") b.textContent = body;
  else b.appendChild(body);
  dlg.appendChild(b);
  const act = el("div", { class: "actions" }, ...actions);
  dlg.appendChild(act);
  scrim.appendChild(dlg);
  scrim.addEventListener("mousedown", (e) => {
    if (e.target === scrim) close();
  });
  function close() {
    scrim.remove();
    document.removeEventListener("keydown", onEsc);
  }
  function onEsc(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      close();
    }
  }
  document.addEventListener("keydown", onEsc);
  document.body.appendChild(scrim);
  return { close };
}

export function confirmDialog(
  title: string,
  body: HTMLElement | string,
  opts: { confirmText: string; danger?: boolean },
): Promise<boolean> {
  return new Promise((resolve) => {
    const done = (v: boolean) => {
      d.close();
      resolve(v);
    };
    const d = dialog(title, body, [
      el("button", { class: "btn ghost", onclick: () => done(false) }, t("action.cancel")),
      el("button", { class: `btn ${opts.danger ? "danger" : "primary"}`, onclick: () => done(true) }, opts.confirmText),
    ]);
  });
}

export function render(node: HTMLElement, into: HTMLElement): void {
  clear(into);
  into.appendChild(node);
}
