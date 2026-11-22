import { el } from "../dom";
import { api, ToolInfo } from "../api";
import { t, tn } from "../i18n";
import { go } from "../router";
import { codeBlock, emptyState, errorState, pill } from "../ui";

export function renderTools(): HTMLElement {
  const root = el("div", { class: "tools-layout" });
  const listPane = el("div", { class: "tools-list" });
  const detailPane = el("div", { class: "tools-detail" });

  const toolbar = el("div", { class: "editor-bar" });
  const search = el("div", { class: "search" });
  const input = el("input", { class: "input", type: "text" });
  input.placeholder = t("field.search_tools");
  search.appendChild(el("span", { class: "ic" }, "⌕"));
  search.appendChild(input);
  toolbar.appendChild(search);
  listPane.appendChild(toolbar);

  const listEl = el("div", { class: "tools-list-body" });
  listPane.appendChild(listEl);
  const foot = el("div", { class: "tools-foot mono" });
  listPane.appendChild(foot);

  root.appendChild(listPane);
  root.appendChild(detailPane);

  let tools: ToolInfo[] = [];
  let selected: string | null = new URLSearchParams(window.location.search).get("tool");

  async function load() {
    try {
      // The catalog is the authoritative source for the callable (read-only)
      // flag; it matches the debug allow-list exactly.
      const catalog = await api.debugCatalog();
      tools = (catalog.tools || []) as ToolInfo[];
      if (!selected && tools.length) selected = tools[0].name;
      drawList();
      drawDetail();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      detailPane.replaceChildren(errorState(t("error.load_failed"), msg, () => void load()));
    }
  }

  function drawList() {
    listEl.replaceChildren();
    const q = input.value.trim().toLowerCase();
    const hits = tools.filter((tt) => tt.name.toLowerCase().includes(q) || tt.description.toLowerCase().includes(q));
    if (!hits.length) {
      listEl.appendChild(emptyState("▦", t("empty.search.title"), t("empty.search.desc")));
    }
    for (const tt of hits) {
      const row = el("div", { class: "tool-row" + (tt.name === selected ? " selected" : "") });
      row.appendChild(el("div", { class: "tool-row-main" },
        el("div", { class: "tool-row-name mono" }, tt.name),
        el("div", { class: "tool-row-desc" }, tt.description),
      ));
      row.appendChild(pill(tt.callable ? "ok" : "outline", tt.callable ? "read" : "write", { mono: true }));
      row.addEventListener("click", () => {
        selected = tt.name;
        go(`/resources/tools?tool=${encodeURIComponent(tt.name)}`);
        drawList();
        drawDetail();
      });
      listEl.appendChild(row);
    }
    foot.textContent = tn("unit.tools", tools.length);
  }

  function drawDetail() {
    detailPane.replaceChildren();
    const tt = tools.find((x) => x.name === selected);
    if (!tt) {
      detailPane.appendChild(emptyState("▦", t("nav.tools"), ""));
      return;
    }
    const head = el("div", { class: "tools-detail-head" });
    head.appendChild(el("div", { class: "row" },
      el("span", { class: "tools-detail-name mono" }, tt.name),
      pill("outline", t("state.readonly")),
    ));
    head.appendChild(el("div", { class: "subtle mono" }, "runvm://tools"));
    detailPane.appendChild(head);

    detailPane.appendChild(el("div", { class: "section-title" }, t("table.col.description")));
    detailPane.appendChild(el("div", { class: "tools-desc" }, tt.description));

    const params = paramRows(tt.inputSchema);
    detailPane.appendChild(el("div", { class: "section-title" }, `${t("meta.params")} (${params.length})`));
    if (params.length === 0) {
      detailPane.appendChild(el("div", { class: "mute" }, t("state.no_params")));
    } else {
      const table = el("table", { class: "table" });
      table.appendChild(el("thead", {}, el("tr", {},
        el("th", {}, t("table.col.field")), el("th", {}, t("table.col.type")),
        el("th", {}, t("table.col.required")), el("th", {}, t("table.col.description")),
      )));
      const tbody = el("tbody");
      for (const p of params) {
        tbody.appendChild(el("tr", {},
          el("td", { class: "mono" }, p.field),
          el("td", { class: "mono" }, p.type),
          el("td", {}, p.required ? "✓" : ""),
          el("td", {}, p.desc),
        ));
      }
      table.appendChild(tbody);
      detailPane.appendChild(table);
    }

    detailPane.appendChild(el("div", { class: "section-title" }, "input schema"));
    detailPane.appendChild(codeBlock(JSON.stringify(tt.inputSchema ?? {}, null, 2), "input schema", true));
  }

  const onKey = (e: KeyboardEvent) => {
    if (e.key === "/" && document.activeElement !== input) {
      e.preventDefault();
      input.focus();
    }
  };
  document.addEventListener("keydown", onKey);
  const timer = setInterval(() => {
    if (!document.body.contains(root)) {
      clearInterval(timer);
      document.removeEventListener("keydown", onKey);
    }
  }, 1000);

  void load();
  return root;
}

function paramRows(schema: any): { field: string; type: string; required: boolean; desc: string }[] {
  if (!schema || schema.type !== "object" || !schema.properties) return [];
  const props = schema.properties as Record<string, any>;
  const required: string[] = Array.isArray(schema.required) ? schema.required : [];
  return Object.entries(props).map(([name, spec]) => ({
    field: name,
    type: schemaType(spec),
    required: required.includes(name),
    desc: spec?.description || "",
  }));
}

function schemaType(spec: any): string {
  if (!spec) return "";
  const t = spec.type;
  if (Array.isArray(t)) return t.join(" | ");
  return t || "";
}
