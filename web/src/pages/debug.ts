import { el, clear } from "../dom";
import { api, ToolInfo, blobURL, b64ToBytes } from "../api";
import { t } from "../i18n";
import { jsonTree, codeBlock, pill, errorState, emptyState } from "../ui";
import { fmtMs, fmtBytes } from "../format";

const METHODS = ["initialize", "ping", "tools/list", "tools/call", "resources/list", "resources/read", "prompts/list", "prompts/get"];

interface HistoryEntry {
  method: string;
  params: unknown;
  time: string;
  ok: boolean;
  resp: unknown;
}

interface ResourceInfo {
  uri: string;
  name?: string;
  description?: string;
  mimeType?: string;
}

interface ConnectionInfo {
  serverInfo?: { name?: string; version?: string };
  protocolVersion?: string;
  capabilities?: Record<string, { listChanged?: boolean }>;
}

export function renderDebug(): HTMLElement {
  const root = el("div", { class: "debug-layout" });

  // connection bar: server identity, negotiated protocol version and the
  // actually-present capabilities are filled from the catalog's initialize
  // answer once it loads (not hard-coded).
  const conn = el("div", { class: "debug-conn" });
  const connServer = el("span", { class: "mono" });
  const connProto = el("span", { class: "mute mono" });
  const connCaps = el("span", { class: "debug-caps" });
  conn.appendChild(el("span", { class: "dot ok" }));
  conn.appendChild(el("span", {}, t("state.connected")));
  conn.appendChild(connServer);
  conn.appendChild(el("span", { class: "mute mono" }, `${t("meta.transport")}: streamable-http`));
  conn.appendChild(el("span", { class: "mute mono" }, `${t("meta.endpoint")}: /mcp`));
  conn.appendChild(connProto);
  conn.appendChild(connCaps);
  conn.appendChild(el("span", { class: "mute" }, t("debug.no_progress")));
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
  let resources: ResourceInfo[] = [];
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
      resources = (catalog.resources || []) as ResourceInfo[];
      prompts = (catalog.prompts || []) as { name: string; description: string }[];
      lint = lintData as typeof lint;
      applyConnInfo((catalog.initialize || {}) as ConnectionInfo);
      if (!tool && tools.some((x) => x.callable)) tool = tools.find((x) => x.callable)!.name;
      renderLeft();
      renderParams();
      renderRight();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      right.replaceChildren(errorState(t("error.load_failed"), msg, () => void load()));
    }
  }

  function applyConnInfo(info: ConnectionInfo) {
    const name = info?.serverInfo?.name || "runvm";
    const ver = info?.serverInfo?.version;
    connServer.textContent = ver ? `${name} v${ver}` : name;
    connProto.textContent = `${t("meta.protocol")}: ${info?.protocolVersion || "—"}`;
    clear(connCaps);
    const caps = info?.capabilities;
    if (!caps) return;
    for (const [cap, spec] of Object.entries(caps)) {
      const changed = spec?.listChanged === true;
      connCaps.appendChild(el("span", {
        class: "debug-cap" + (changed ? " changed" : ""),
        title: `${cap} · listChanged: ${changed}`,
      }, cap));
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
    for (const r of resources) {
      const row = el("div", { class: "dbg-row res" }, resRow(r.name || r.uri, r.description, r.uri));
      row.addEventListener("click", () => {
        method = "resources/read";
        paramsJSON = JSON.stringify({ uri: r.uri }, null, 2);
        renderLeft();
        renderParams();
      });
      left.appendChild(row);
    }
    left.appendChild(groupTitle(t("debug.prompts")));
    for (const p of prompts) {
      const row = el("div", { class: "dbg-row res" }, resRow(p.name, p.description));
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

  // resRow stacks a resource/prompt's name, description and (for resources)
  // URI into one clickable row, so the left column shows more than a bare id.
  function resRow(title: string, desc?: string, uri?: string): HTMLElement {
    const col = el("div", { class: "dbg-row-main" });
    col.appendChild(el("span", { class: "mono" }, title));
    if (desc) col.appendChild(el("span", { class: "mute" }, desc));
    if (uri) col.appendChild(el("span", { class: "mono subtle" }, uri));
    return col;
  }

  // --- params (middle) ---

  let paramsJSON = "";
  let formMode = true; // form vs JSON editor for tools/call
  const midTabs = el("div", { class: "tabs" });
  const formTab = el("button", { class: "active" }, t("debug.form"));
  const jsonTab = el("button", {}, t("debug.json"));
  midTabs.appendChild(formTab);
  midTabs.appendChild(jsonTab);

  const formWrap = el("div", { class: "dbg-params-form" });
  const jsonArea = el("textarea", { class: "textarea mono", spellcheck: "false" });

  const sendBtn = el("button", { class: "btn primary" }, t("action.send"));
  const elapsed = el("span", { class: "mute mono" }, "");
  const notCallableWarn = el("div", { class: "dbg-warn" });

  function renderParams() {
    clear(formWrap);
    formWrap.appendChild(el("div", { class: "dbg-field" },
      el("label", { class: "dbg-label" }, t("meta.method")),
      el("span", { class: "mono" }, method)));

    const isCall = method === "tools/call";
    // The form/JSON tabs only make sense when there is a schema form to
    // switch from; every other method uses the JSON editor as its params view.
    midTabs.style.display = isCall ? "" : "none";

    let selectedTool = tools.find((x) => x.name === tool);
    if (isCall) {
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
      formWrap.appendChild(el("div", { class: "dbg-field" },
        el("label", { class: "dbg-label" }, "tool"),
        toolSel));
      selectedTool = tools.find((x) => x.name === tool);
      if (selectedTool) buildSchemaForm(selectedTool.inputSchema, formWrap);
      else formWrap.appendChild(el("div", { class: "mute" }, t("state.no_params")));

      formTab.className = formMode ? "active" : "";
      jsonTab.className = formMode ? "" : "active";
      formWrap.style.display = formMode ? "" : "none";
      jsonArea.style.display = formMode ? "none" : "";
    } else {
      // No schema form: the JSON editor is the params view, always visible
      // and kept in sync with the selected method/resource/prompt.
      formWrap.style.display = "none";
      jsonArea.style.display = "";
      jsonArea.value = paramsJSON;
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
      const response = (lastResp.response as any);
      const result = response?.result ?? response?.error;
      if (response?.error) {
        // A JSON-RPC error renders as a plain tree (code + message), never as
        // an image or a downloadable resource.
        rightBody.appendChild(jsonTree(result));
        return;
      }
      if (result == null) {
        rightBody.appendChild(emptyState("▤", t("debug.history"), ""));
        return;
      }
      for (const node of renderResultNodes(result, method, lastResp)) rightBody.appendChild(node);
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

  // --- result rendering helpers ---

  function renderResultNodes(result: any, method: string, last: { request: unknown; response: unknown }): HTMLElement[] {
    // tools/call → a callResult with a `content` array that may mix text and
    // base64 images; resources/read → `contents[0]` as text or blob.
    if (method === "tools/call" && Array.isArray(result?.content)) {
      return renderCallContent(result.content);
    }
    if (method === "resources/read" && Array.isArray(result?.contents) && result.contents.length > 0) {
      const params = (last.request as any)?.params;
      const uri = typeof params?.uri === "string" ? params.uri : "";
      return renderResourceContent(result.contents[0], uri);
    }
    return [jsonTree(result)];
  }

  function renderCallContent(content: any[]): HTMLElement[] {
    const out: HTMLElement[] = [];
    for (const item of content) {
      if (item?.type === "image") out.push(renderImage(item.data, item.mimeType));
      else if (item?.type === "text") out.push(el("pre", { class: "codeblock" }, String(item.text ?? "")));
      else out.push(jsonTree(item));
    }
    return out.length ? out : [jsonTree({ content })];
  }

  function renderResourceContent(entry: any, uri: string): HTMLElement[] {
    const mime = String(entry?.mimeType || "");
    if (entry?.blob) {
      if (mime.startsWith("image/")) return [renderImage(entry.blob, mime)];
      return [renderBinary(entry.blob, mime, uri)];
    }
    if (entry?.text != null) {
      if (mime === "application/json" || mime.endsWith("+json")) {
        let parsed: unknown;
        try {
          parsed = JSON.parse(String(entry.text));
        } catch {
          return [el("pre", { class: "codeblock" }, String(entry.text))];
        }
        return [jsonTree(parsed)];
      }
      return [el("pre", { class: "codeblock" }, String(entry.text))];
    }
    return [jsonTree(entry)];
  }

  // renderImage turns a base64 blob into an <img> and keeps a way back to the
  // raw wire bytes (the raw tab) — the debug page's "view original" affordance.
  function renderImage(data: string, mime: string): HTMLElement {
    const wrap = el("div", { class: "dbg-img" });
    const holder = el("div", { class: "img-preview" });
    holder.appendChild(el("img", { src: blobURL(data, mime || "image/png"), decoding: "async" }));
    wrap.appendChild(holder);
    const bar = el("div", { class: "dbg-img-bar" });
    bar.appendChild(el("span", { class: "mute mono" }, mime || "image/png"));
    bar.appendChild(el("span", { class: "spacer" }));
    bar.appendChild(el("button", { class: "btn ghost sm", onclick: () => { rightTab = "raw"; syncRightTabs(); renderRight(); } }, t("debug.view_raw")));
    wrap.appendChild(bar);
    return wrap;
  }

  // renderBinary shows non-image blobs (PDF, unknown) as metadata + a download
  // link instead of a JSON tree full of base64.
  function renderBinary(data: string, mime: string, uri: string): HTMLElement {
    const box = el("div", { class: "binary-info" });
    const bytes = b64ToBytes(data);
    const rows: [string, string][] = [
      [t("table.col.type"), mime || "application/octet-stream"],
      [t("table.col.size"), fmtBytes(bytes.length)],
    ];
    for (const [k, v] of rows) {
      box.appendChild(el("div", { class: "binary-row" },
        el("span", { class: "mute" }, k), el("span", { class: "mono" }, v)));
    }
    box.appendChild(el("a", { href: blobURL(data, mime || "application/octet-stream"), download: basename(uri) },
      el("button", { class: "btn ghost sm" }, t("action.download"))));
    return box;
  }

  function basename(uri: string): string {
    const seg = uri.split("?")[0].split("/").filter(Boolean).pop() || "";
    return seg || "resource";
  }

  formTab.addEventListener("click", () => {
    formMode = true;
    renderParams();
  });
  jsonTab.addEventListener("click", () => {
    formMode = false;
    renderParams();
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
