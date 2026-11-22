import { el, clear } from "../dom";
import { api, ToolInfo } from "../api";
import { t } from "../i18n";
import { jsonTree, codeBlock, pill, errorState, emptyState } from "../ui";
import { fmtMs } from "../format";

const METHODS = ["initialize", "ping", "tools/list", "tools/call", "resources/list", "resources/read", "prompts/list", "prompts/get"];

interface HistoryEntry {
  method: string;
  params: unknown;
  time: string;
  ok: boolean;
  resp: unknown;
}

export function renderDebug(): HTMLElement {
  const root = el("div", { class: "debug-layout" });

  // connection bar
  const conn = el("div", { class: "debug-conn" });
  conn.appendChild(el("span", { class: "dot ok" }));
  conn.appendChild(el("span", {}, t("state.connected")));
  conn.appendChild(el("span", { class: "mute mono" }, `${t("meta.transport")}: streamable-http`));
  conn.appendChild(el("span", { class: "mute mono" }, `${t("meta.endpoint")}: /mcp`));
  conn.appendChild(el("span", { class: "mute mono" }, `${t("meta.protocol")}: 2025-06-18`));
  conn.appendChild(el("span", { class: "spacer" }));
  conn.appendChild(pill("ok", t("state.readonly")));
  root.appendChild(conn);

  const body = el("div", { class: "debug-body" });

  // left column
  const left = el("div", { class: "debug-left" });
  const mid = el("div", { class: "debug-mid" });
  const right = el("div", { class: "debug-right" });
  body.appendChild(left);
  body.appendChild(mid);
  body.appendChild(right);
  root.appendChild(body);

  let tools: ToolInfo[] = [];
  let resources: string[] = [];
  let prompts: { name: string; description: string }[] = [];
  let lint: { issues: any[]; errors: number; warnings: number } = { issues: [], errors: 0, warnings: 0 };
  let method = "tools/list";
  let tool: string | null = null;
  let history: HistoryEntry[] = [];
  let lastResp: { request: unknown; response: unknown } | null = null;

  async function load() {
    try {
      const [catalog, lintData] = await Promise.all([api.debugCatalog(), api.debugLint()]);
      tools = (catalog.tools || []) as ToolInfo[];
      resources = (catalog.resources || []).map((r: any) => r.uri);
      prompts = (catalog.prompts || []) as { name: string; description: string }[];
      lint = lintData as typeof lint;
      if (!tool && tools.some((x) => x.callable)) tool = tools.find((x) => x.callable)!.name;
      renderLeft();
      renderParams();
      renderRight();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      right.replaceChildren(errorState(t("error.load_failed"), msg, () => void load()));
    }
  }

  function renderLeft() {
    clear(left);
    left.appendChild(groupTitle(t("debug.methods")));
    for (const m of METHODS) {
      const row = el("div", { class: "dbg-row" + (method === m ? " selected" : "") }, el("span", { class: "mono" }, m));
      row.addEventListener("click", () => {
        method = m;
        if (m === "resources/read" && resources.length) paramsJSON = JSON.stringify({ uri: resources[0] }, null, 2);
        if (m === "prompts/get" && prompts.length) paramsJSON = JSON.stringify({ name: prompts[0].name }, null, 2);
        renderLeft();
        renderParams();
      });
      left.appendChild(row);
    }

    const callable = tools.filter((x) => x.callable);
    left.appendChild(groupTitle(t("debug.tools_readonly")));
    for (const tt of callable) {
      const row = el("div", { class: "dbg-row" + (method === "tools/call" && tool === tt.name ? " selected" : "") },
        el("span", { class: "mono" }, tt.name));
      row.addEventListener("click", () => {
        method = "tools/call";
        tool = tt.name;
        paramsJSON = "";
        renderLeft();
        renderParams();
      });
      left.appendChild(row);
    }

    const notCallable = tools.filter((x) => !x.callable);
    left.appendChild(groupTitle(`${t("debug.tools_not_callable")} (${notCallable.length})`));
    for (const tt of notCallable) {
      const row = el("div", { class: "dbg-row disabled", title: t("debug.not_callable_hint") },
        el("span", { class: "mono" }, tt.name),
        pill("outline", t("debug.readonly_page")));
      left.appendChild(row);
    }

    left.appendChild(groupTitle(t("debug.resources")));
    for (const uri of resources) {
      const row = el("div", { class: "dbg-row" }, el("span", { class: "mono subtle" }, uri));
      row.addEventListener("click", () => {
        method = "resources/read";
        paramsJSON = JSON.stringify({ uri }, null, 2);
        renderLeft();
        renderParams();
      });
      left.appendChild(row);
    }
    left.appendChild(groupTitle(t("debug.prompts")));
    for (const p of prompts) {
      const row = el("div", { class: "dbg-row" }, el("span", { class: "mono" }, p.name));
      row.addEventListener("click", () => {
        method = "prompts/get";
        paramsJSON = JSON.stringify({ name: p.name }, null, 2);
        renderLeft();
        renderParams();
      });
      left.appendChild(row);
    }

    // history
    left.appendChild(groupTitle(t("debug.history")));
    for (const h of history) {
      const row = el("div", { class: "dbg-row hist" },
        el("span", { class: "mono" }, h.time),
        el("span", { class: "mono" }, h.method),
        el("span", { class: `dot ${h.ok ? "ok" : "danger"}` }));
      row.addEventListener("click", () => {
        method = h.method;
        paramsJSON = JSON.stringify(h.params, null, 2);
        lastResp = h.resp as { request: unknown; response: unknown };
        renderLeft();
        renderParams();
        renderRight();
      });
      left.appendChild(row);
    }
  }

  function groupTitle(text: string): HTMLElement {
    return el("div", { class: "dbg-group" }, text);
  }

  // --- params (middle) ---

  let paramsJSON = "";
  const midTabs = el("div", { class: "tabs" });
  const formTab = el("button", { class: "active" }, t("debug.form"));
  const jsonTab = el("button", {}, t("debug.json"));
  midTabs.appendChild(formTab);
  midTabs.appendChild(jsonTab);

  const formWrap = el("div", { class: "dbg-params-form" });
  const jsonArea = el("textarea", { class: "textarea mono" });
  jsonArea.style.display = "none";

  const sendBtn = el("button", { class: "btn primary" }, t("action.send"));
  const elapsed = el("span", { class: "mute mono" }, "");
  const notCallableWarn = el("div", { class: "dbg-warn" });

  function renderParams() {
    clear(formWrap);
    const methodLabel = el("div", { class: "dbg-field" },
      el("label", { class: "dbg-label" }, t("meta.method")),
      el("span", { class: "mono" }, method));
    formWrap.appendChild(methodLabel);

    let selectedTool = tools.find((x) => x.name === tool);
    if (method === "tools/call") {
      const toolSel = el("select", { class: "select" });
      for (const tt of tools.filter((x) => x.callable)) {
        const o = el("option", { value: tt.name }, tt.name) as HTMLOptionElement;
        if (tt.name === tool) o.selected = true;
        toolSel.appendChild(o);
      }
      toolSel.addEventListener("change", () => {
        tool = toolSel.value;
        renderParams();
      });
      const toolField = el("div", { class: "dbg-field" },
        el("label", { class: "dbg-label" }, "tool"),
        toolSel);
      formWrap.appendChild(toolField);
      selectedTool = tools.find((x) => x.name === tool);
    }

    if (selectedTool && method === "tools/call") {
      buildSchemaForm(selectedTool.inputSchema, formWrap);
    } else {
      formWrap.appendChild(el("div", { class: "mute" }, t("debug.json")));
      formWrap.appendChild(jsonArea);
    }
  }

  function buildSchemaForm(schema: any, wrap: HTMLElement) {
    if (!schema || schema.type !== "object" || !schema.properties) {
      wrap.appendChild(el("div", { class: "mute" }, t("state.no_params")));
      return;
    }
    const props = schema.properties as Record<string, any>;
    for (const [name, spec] of Object.entries(props)) {
      const ttype = Array.isArray(spec?.type) ? spec.type[0] : spec?.type;
      let ctrl: HTMLElement;
      if (ttype === "boolean") {
        ctrl = el("input", { type: "checkbox" });
      } else if (ttype === "number" || ttype === "integer") {
        ctrl = el("input", { class: "input", type: "number" });
      } else {
        ctrl = el("input", { class: "input", type: "text" });
      }
      ctrl.setAttribute("data-field", name);
      wrap.appendChild(el("div", { class: "dbg-field" },
        el("label", { class: "dbg-label" }, name),
        ctrl));
    }
  }

  function collectParams(): unknown {
    if (method !== "tools/call") {
      try {
        return paramsJSON.trim() ? JSON.parse(paramsJSON) : {};
      } catch {
        return paramsJSON.trim() ? paramsJSON : {};
      }
    }
    const args: Record<string, unknown> = {};
    formWrap.querySelectorAll<HTMLElement>("[data-field]").forEach((ctrl) => {
      const name = ctrl.getAttribute("data-field")!;
      if (ctrl instanceof HTMLInputElement && ctrl.type === "checkbox") args[name] = ctrl.checked;
      else if (ctrl instanceof HTMLInputElement && ctrl.type === "number") args[name] = ctrl.value === "" ? undefined : Number(ctrl.value);
      else if (ctrl instanceof HTMLInputElement) args[name] = ctrl.value === "" ? undefined : ctrl.value;
    });
    return { name: tool, arguments: args };
  }

  async function send() {
    const params = collectParams();
    const started = performance.now();
    sendBtn.disabled = true;
    try {
      const resp = await api.debug({ jsonrpc: "2.0", id: 1, method, params });
      const ms = performance.now() - started;
      elapsed.textContent = fmtMs(ms);
      lastResp = resp as { request: unknown; response: unknown };
      const rerr = (resp.response as any)?.error;
      history.unshift({
        method,
        params,
        time: new Date().toISOString().slice(11, 19),
        ok: !rerr,
        resp: resp,
      });
      if (history.length > 20) history.pop();
      renderLeft();
      renderRight();
    } catch (e) {
      elapsed.textContent = "";
      notCallableWarn.textContent = e instanceof Error ? e.message : String(e);
    } finally {
      sendBtn.disabled = false;
    }
  }

  // --- result (right) ---

  const rightTabs = el("div", { class: "tabs" });
  const resultTab = el("button", { class: "active" }, t("meta.result"));
  const rawTab = el("button", {}, t("meta.raw"));
  const lintTab = el("button", {}, `${t("debug.schema_check")} (${0})`);
  rightTabs.appendChild(resultTab);
  rightTabs.appendChild(rawTab);
  rightTabs.appendChild(lintTab);
  let rightTab: "result" | "raw" | "lint" = "result";
  const rightBody = el("div", { class: "dbg-right-body" });

  function renderRight() {
    lintTab.textContent = `${t("debug.schema_check")} (${lint.issues.length})`;
    clear(rightBody);
    if (rightTab === "lint") {
      renderLint();
      return;
    }
    if (!lastResp) {
      rightBody.appendChild(emptyState("▤", t("debug.history"), ""));
      return;
    }
    if (rightTab === "result") {
      const result = (lastResp.response as any)?.result ?? (lastResp.response as any)?.error;
      if (result != null) rightBody.appendChild(jsonTree(result));
    } else {
      rightBody.appendChild(codeBlock(JSON.stringify(lastResp.request, null, 2), t("meta.request"), true));
      rightBody.appendChild(codeBlock(JSON.stringify(lastResp.response, null, 2), t("meta.response"), true));
    }
  }

  function renderLint() {
    if (!lint.issues.length) {
      rightBody.appendChild(el("div", { class: "mute" }, t("debug.portability_ok")));
      return;
    }
    for (const issue of lint.issues) {
      const row = el("div", { class: "lint-row" });
      row.appendChild(pill(issue.severity === "error" ? "danger" : "warn", issue.severity === "error" ? "ERROR" : "WARN", { mono: true }));
      row.appendChild(el("span", { class: "mono" }, issue.tool));
      row.appendChild(el("span", { class: "mono mute" }, issue.rule));
      row.appendChild(el("span", { class: "mono subtle" }, issue.path));
      rightBody.appendChild(row);
    }
  }

  formTab.addEventListener("click", () => {
    formTab.className = "active";
    jsonTab.className = "";
    formWrap.style.display = "";
    jsonArea.style.display = "none";
  });
  jsonTab.addEventListener("click", () => {
    jsonTab.className = "active";
    formTab.className = "";
    formWrap.style.display = "none";
    jsonArea.style.display = "";
  });
  resultTab.addEventListener("click", () => { rightTab = "result"; syncRightTabs(); renderRight(); });
  rawTab.addEventListener("click", () => { rightTab = "raw"; syncRightTabs(); renderRight(); });
  lintTab.addEventListener("click", () => { rightTab = "lint"; syncRightTabs(); renderRight(); });
  function syncRightTabs() {
    resultTab.className = rightTab === "result" ? "active" : "";
    rawTab.className = rightTab === "raw" ? "active" : "";
    lintTab.className = rightTab === "lint" ? "active" : "";
  }

  jsonArea.addEventListener("input", () => (paramsJSON = jsonArea.value));

  sendBtn.addEventListener("click", () => void send());
  const sendBar = el("div", { class: "dbg-sendbar" });
  sendBar.appendChild(sendBtn);
  sendBar.appendChild(elapsed);
  sendBar.appendChild(el("span", { class: "spacer" }));
  sendBar.appendChild(notCallableWarn);
  sendBar.appendChild(el("button", { class: "btn ghost sm", onclick: () => { paramsJSON = ""; renderParams(); } }, t("action.reset")));

  mid.appendChild(el("div", { class: "dbg-mid-title mono" }, t("meta.params")));
  mid.appendChild(midTabs);
  mid.appendChild(formWrap);
  mid.appendChild(jsonArea);
  mid.appendChild(sendBar);

  right.appendChild(rightTabs);
  right.appendChild(rightBody);

  const timer = setInterval(() => {
    if (!document.body.contains(root)) clearInterval(timer);
  }, 1000);

  void load();
  return root;
}
