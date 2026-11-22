import { el } from "../dom";
import { api, loadTools, ResourceEntry, ToolInfo } from "../api";
import { t, tn } from "../i18n";
import { fmtBytes } from "../format";
import { go } from "../router";
import { emptyState, errorState, pill } from "../ui";

interface Shot {
  uri: string;
  session: string;
  target: string;
}

export function renderSearch(): HTMLElement {
  const root = el("div", { class: "page search-page" });
  const searchBox = el("div", { class: "search lg" });
  const mag = el("span", { class: "ic" }, "⌕");
  const input = el("input", { class: "input", type: "text" });
  input.placeholder = t("field.search_all");
  const kbd = el("span", { class: "kbd-hint" }, el("kbd", { class: "kbd" }, "/"));
  searchBox.appendChild(mag);
  searchBox.appendChild(input);
  searchBox.appendChild(kbd);
  const boxWrap = el("div", { class: "search-box-wrap" }, searchBox);
  root.appendChild(boxWrap);

  const results = el("div", { class: "search-results" });
  root.appendChild(results);

  let tools: ToolInfo[] = [];
  let shots: Shot[] = [];
  let files: ResourceEntry[] = [];

  async function load() {
    try {
      const [tList, res] = await Promise.all([loadTools(), api.resources()]);
      tools = tList;
      const resources = res.resources || [];
      shots = resources
        .filter((r) => r.uri.startsWith("runvm://screenshot/"))
        .map((r) => ({ uri: r.uri, session: r.uri.slice("runvm://screenshot/".length), target: r.description || "" }));
      files = resources.filter((r) => r.uri.startsWith("runvm://files/"));
      draw();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      results.replaceChildren(errorState(t("error.load_failed"), msg, () => void load()));
    }
  }

  let debounce: number | undefined;
  input.addEventListener("input", () => {
    window.clearTimeout(debounce);
    debounce = window.setTimeout(draw, 120);
  });

  function draw() {
    results.replaceChildren();
    const q = input.value.trim().toLowerCase();
    if (!q) {
      drawEmpty();
      return;
    }
    const toolHits = tools.filter(
      (tt) =>
        tt.name.toLowerCase().includes(q) ||
        tt.description.toLowerCase().includes(q) ||
        JSON.stringify(tt.inputSchema).toLowerCase().includes(q),
    );
    const shotHits = shots.filter((s) => s.session.toLowerCase().includes(q) || s.target.toLowerCase().includes(q));
    const fileHits = files.filter(
      (f) => f.uri.toLowerCase().includes(q) || (f.name || "").toLowerCase().includes(q),
    );

    let any = false;
    if (toolHits.length) {
      any = true;
      results.appendChild(groupHeader(t("search.group_tools"), tn("unit.tools", toolHits.length)));
      for (const tt of toolHits) results.appendChild(toolRow(tt));
    }
    if (shotHits.length) {
      any = true;
      results.appendChild(groupHeader(t("search.group_screenshots"), tn("unit.screenshots", shotHits.length)));
      for (const s of shotHits) results.appendChild(shotRow(s));
    }
    if (fileHits.length) {
      any = true;
      results.appendChild(groupHeader(t("search.group_files"), tn("unit.files", fileHits.length)));
      for (const f of fileHits) results.appendChild(fileRow(f));
    }
    if (!any) {
      results.appendChild(emptyState("⌕", t("empty.search.title"), t("empty.search.desc")));
    }
  }

  function groupHeader(label: string, count: string): HTMLElement {
    const h = el("div", { class: "search-group" });
    h.appendChild(el("span", { class: "search-group-label" }, label));
    h.appendChild(el("span", { class: "search-group-count mono" }, count));
    return h;
  }

  function toolRow(tt: ToolInfo): HTMLElement {
    const row = el("div", { class: "search-row" });
    row.addEventListener("click", () => go(`/resources/tools?tool=${encodeURIComponent(tt.name)}`));
    row.appendChild(el("div", { class: "search-row-main" },
      el("div", { class: "search-row-name mono" }, tt.name),
      el("div", { class: "search-row-desc" }, tt.description),
    ));
    row.appendChild(el("div", { class: "search-row-meta" },
      pill("neutral", "JSON", { mono: true }),
      el("span", { class: "mono subtle" }, "runvm://tools"),
    ));
    return row;
  }

  function shotRow(s: Shot): HTMLElement {
    const row = el("div", { class: "search-row" });
    row.addEventListener("click", () => go(`/resources/screenshots?session=${encodeURIComponent(s.session)}`));
    row.appendChild(el("div", { class: "search-row-main" },
      el("div", { class: "search-row-name mono" }, s.session + ".png"),
      el("div", { class: "search-row-desc" }, s.target),
    ));
    row.appendChild(el("div", { class: "search-row-meta" },
      pill("neutral", "PNG", { mono: true }),
    ));
    return row;
  }

  function fileRow(f: ResourceEntry): HTMLElement {
    const row = el("div", { class: "search-row" });
    row.addEventListener("click", () => go(`/resources/files?file=${encodeURIComponent(f.name || "")}`));
    row.appendChild(el("div", { class: "search-row-main" },
      el("div", { class: "search-row-name mono" }, f.uri),
      el("div", { class: "search-row-desc mono" }, `${fmtBytes(f.size || 0)} · ${ext(f.name || "") || "file"}`),
    ));
    row.appendChild(el("div", { class: "search-row-meta" },
      f.writable ? pill("ok", t("state.writable"), { dot: "ok" }) : pill("outline", t("state.readonly")),
    ));
    return row;
  }

  function drawEmpty() {
    const total = {
      tools: tools.length,
      shots: shots.length,
      files: files.length,
    };
    const cards = el("div", { class: "card-grid search-entry-cards" });
    cards.appendChild(entryCard("▦", t("nav.tools"), tn("unit.tools", total.tools), "/resources/tools"));
    cards.appendChild(entryCard("▣", t("nav.screenshots"), tn("unit.screenshots", total.shots), "/resources/screenshots"));
    cards.appendChild(entryCard("▤", t("nav.files"), tn("unit.files", total.files), "/resources/files"));
    results.appendChild(cards);
  }

  function entryCard(icon: string, label: string, count: string, path: string): HTMLElement {
    const card = el("div", { class: "card entry-card" });
    card.addEventListener("click", () => go(path));
    card.appendChild(el("div", { class: "entry-ic" }, icon));
    card.appendChild(el("div", { class: "entry-label" }, label));
    card.appendChild(el("div", { class: "entry-count mono" }, count));
    return card;
  }

  function ext(name: string): string {
    const i = name.lastIndexOf(".");
    return i >= 0 ? name.slice(i + 1) : "";
  }

  // focus shortcut
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "/" && document.activeElement !== input) {
      e.preventDefault();
      input.focus();
    }
  };
  document.addEventListener("keydown", onKey);
  const cleanup = () => document.removeEventListener("keydown", onKey);
  // self-cleanup when detached
  const timer = setInterval(() => {
    if (!document.body.contains(root)) {
      clearInterval(timer);
      cleanup();
    }
  }, 1000);

  void load();
  return root;
}
