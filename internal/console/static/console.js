"use strict";

const $ = (sel) => document.querySelector(sel);

function el(tag, attrs, ...children) {
  const node = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (k === "class") node.className = v;
      else if (k === "html") node.innerHTML = v;
      else if (k.startsWith("on")) node.addEventListener(k.slice(2), v);
      else if (v !== undefined && v !== null) node.setAttribute(k, v);
    }
  }
  appendChildren(node, children);
  return node;
}

// appendChildren appends nodes, strings, and (recursively) arrays of them, so
// callers can pass rows.map(...) straight into el() instead of spreading first.
function appendChildren(node, children) {
  for (const c of children) {
    if (c == null) continue;
    if (Array.isArray(c)) {
      appendChildren(node, c);
    } else if (c.nodeType) {
      node.append(c);
    } else {
      node.append(document.createTextNode(String(c)));
    }
  }
}

async function api(path, opts = {}) {
  const init = { method: opts.method || "GET", headers: opts.headers || {} };
  if (opts.body !== undefined) {
    if (opts.body instanceof FormData) {
      init.body = opts.body;
    } else {
      init.headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(opts.body);
    }
  }
  const resp = await fetch(path, init);
  if (resp.status === 401 && path !== "/api/login") {
    showLogin();
    throw new Error("未登录");
  }
  let data = null;
  const ct = resp.headers.get("Content-Type") || "";
  if (ct.includes("json")) {
    try { data = await resp.json(); } catch (_) { data = null; }
  }
  if (!resp.ok) {
    const msg = (data && (data.error || data.message)) || (data && JSON.stringify(data)) || resp.statusText;
    throw new Error(msg);
  }
  return data;
}

function showLogin() {
  $("#app-view").classList.add("hidden");
  $("#login-view").classList.remove("hidden");
}

function showApp() {
  $("#login-view").classList.add("hidden");
  $("#app-view").classList.remove("hidden");
  switchTab("status");
  refreshStatus();
  refreshResources();
  refreshDebug();
}

function switchTab(name) {
  document.querySelectorAll("nav button[data-tab]").forEach((b) => {
    b.classList.toggle("active", b.dataset.tab === name);
  });
  document.querySelectorAll(".tab").forEach((s) => {
    s.classList.toggle("active", s.id === "tab-" + name);
  });
}

function pretty(v) {
  if (v === undefined || v === null) return String(v);
  if (typeof v === "string") return v;
  return JSON.stringify(v, null, 2);
}

// ---- auth ----

$("#login-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const fd = new FormData(e.target);
  $("#login-error").classList.add("hidden");
  try {
    await api("/api/login", {
      method: "POST",
      body: { username: fd.get("username"), password: fd.get("password") },
    });
    showApp();
  } catch (err) {
    $("#login-error").textContent = err.message;
    $("#login-error").classList.remove("hidden");
  }
});

$("#logout").addEventListener("click", async () => {
  try { await api("/api/logout", { method: "POST" }); } catch (_) {}
  showLogin();
});

// ---- tabs ----

document.querySelectorAll("nav button[data-tab]").forEach((b) => {
  b.addEventListener("click", () => switchTab(b.dataset.tab));
});

// ---- status ----

async function refreshStatus() {
  try {
    const st = await api("/api/status");
    const body = $("#status-body");
    body.replaceChildren();

    const a = st.android || {};
    const i = st.ios || {};
    const h5 = st.h5 || {};
    const limits = st.limits || {};
    const sessions = st.sessions || {};

    body.append(el("div", { class: "grid" },
      statCard("android 会话", a.sessions ?? 0, a.assets_ok ? "资源就绪" : "资源缺失"),
      statCard("ios 会话", i.sessions ?? 0),
      statCard("h5 会话", h5.sessions ?? 0),
      statCard("会话上限", limits.max_sessions ?? 0, "0 = 不限"),
      statCard("空闲超时", limits.idle_timeout ?? "0s"),
      statCard("版本", st.version ?? "", st.go + " " + st.os + "/" + st.arch),
    ));

    const androidList = sessions.android || [];
    const iosList = sessions.ios || [];
    const h5List = sessions.h5 || [];
    if (androidList.length || iosList.length || h5List.length) {
      body.append(el("h3", {}, "活跃会话"));
      const t = el("table", {},
        el("thead", {}, el("tr", {},
          el("th", {}, "类型"), el("th", {}, "会话"), el("th", {}, "详情"))),
        el("tbody", { id: "session-rows" }),
      );
      body.append(t);
      const rows = t.querySelector("#session-rows");
      for (const s of androidList) {
        const eng = (s.engines || {});
        rows.append(el("tr", {},
          el("td", {}, "android"),
          el("td", { class: "mono" }, s.session),
          el("td", {}, `pool=${s.pool} in_use=${eng.in_use ?? 0} idle=${eng.idle ?? 0} created=${eng.created ?? 0}`),
        ));
      }
      for (const s of iosList) rows.append(el("tr", {}, el("td", {}, "ios"), el("td", { class: "mono" }, s), el("td", {}, "")));
      for (const s of h5List) rows.append(el("tr", {}, el("td", {}, "h5"), el("td", { class: "mono" }, s), el("td", {}, "")));
    }
  } catch (err) {
    $("#status-body").replaceChildren(el("p", { class: "error" }, String(err.message)));
  }
}

function statCard(k, v, sub) {
  return el("div", { class: "stat" },
    el("div", { class: "k" }, k),
    el("div", { class: "v" }, String(v)),
    sub ? el("div", { class: "muted" }, sub) : null,
  );
}

// ---- resources ----

let currentResource = null;

async function refreshResources() {
  try {
    const data = await api("/api/resources");
    const body = $("#resources-body");
    body.replaceChildren();
    const rows = (data.resources || []);
    if (!rows.length) {
      body.append(el("p", { class: "muted" }, "暂无资源（上传或写入后 runvm://files/ 资源会出现在这里）。"));
      return;
    }
    body.append(el("table", {},
      el("thead", {}, el("tr", {},
        el("th", {}, "URI"), el("th", {}, "大小"), el("th", {}, "类型"), el("th", {}, "读写"), el("th", {}, ""))),
      el("tbody", {}, rows.map((r) => {
        const writable = !!r.writable;
        return el("tr", {},
          el("td", { class: "mono" }, r.uri),
          el("td", {}, r.size !== undefined ? fmtSize(r.size) : "—"),
          el("td", { class: "mono" }, (r.mimeType || r.mime_type || "—").split(";")[0]),
          el("td", {}, el("span", { class: "badge " + (writable ? "writable" : "readonly") }, writable ? "可写" : "只读")),
          el("td", {}, el("button", { onclick: () => openResource(r.uri, writable) }, "查看")),
        );
      })),
    ));
  } catch (err) {
    $("#resources-body").replaceChildren(el("p", { class: "error" }, String(err.message)));
  }
}

async function openResource(uri, writable) {
  const detail = $("#resource-detail");
  detail.classList.remove("hidden");
  detail.replaceChildren(el("p", { class: "muted" }, "加载中…"));
  try {
    const data = await api("/api/resources/read?uri=" + encodeURIComponent(uri));
    detail.replaceChildren();
    detail.append(el("h3", {}, uri));
    detail.append(el("p", { class: "muted" }, (data.mimeType || "") + (writable ? " · 可写" : " · 只读")));

    if (data.text !== undefined) {
      const pre = el("pre", {}, data.text);
      detail.append(pre);
      if (writable) {
        currentResource = { uri: uri, text: data.text };
        detail.append(el("button", { class: "primary", onclick: startEdit }, "编辑"));
        detail.append(el("div", { id: "edit-box", class: "hidden" }));
      }
    } else if (data.blob !== undefined) {
      const mime = data.mimeType || "application/octet-stream";
      const img = el("img", { class: "preview", src: "data:" + mime + ";base64," + data.blob });
      img.onerror = () => { img.remove(); };
      detail.append(img);
      detail.append(el("p", {}, el("a", {
        href: "data:" + mime + ";base64," + data.blob,
        download: uri.split("/").pop() || "resource",
      }, "下载")));
    } else {
      detail.append(el("p", { class: "muted" }, "（无内容）"));
    }
  } catch (err) {
    detail.replaceChildren(el("p", { class: "error" }, String(err.message)));
  }
}

function startEdit() {
  const box = $("#edit-box");
  box.classList.remove("hidden");
  box.replaceChildren(
    el("textarea", { id: "edit-text" }, currentResource.text),
    el("div", { class: "actions" },
      el("button", { class: "primary", onclick: saveEdit }, "保存"),
      el("button", { onclick: () => box.classList.add("hidden") }, "取消"),
    ),
    el("p", { id: "edit-result", class: "hidden" }),
  );
}

async function saveEdit() {
  const name = currentResource.uri.replace(/^runvm:\/\/files\//, "");
  const content = $("#edit-text").value;
  try {
    await api("/api/resources/file", { method: "PUT", body: { name: name, content: content } });
    const result = $("#edit-result");
    result.textContent = "已保存";
    result.classList.remove("hidden");
    $("#edit-box").classList.add("hidden");
    await refreshResources();
    await openResource(currentResource.uri, true);
  } catch (err) {
    const result = $("#edit-result");
    result.textContent = String(err.message);
    result.classList.remove("hidden");
  }
}

function fmtSize(n) {
  if (n < 1024) return n + " B";
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KiB";
  return (n / 1024 / 1024).toFixed(1) + " MiB";
}

// ---- upload ----

$("#upload-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const fd = new FormData(e.target);
  const result = $("#upload-result");
  result.classList.add("hidden");
  try {
    const meta = await api("/api/upload", { method: "POST", body: fd });
    result.textContent = "已上传：" + meta.uri + "（" + meta.size + " 字节, sha256 " + meta.sha256 + "）";
    result.classList.remove("hidden");
    e.target.reset();
    await refreshResources();
  } catch (err) {
    result.textContent = String(err.message);
    result.classList.remove("hidden");
  }
});

// ---- MCP debug ----

let debugId = 1;

async function refreshDebug() {
  try {
    const [catalog, lint] = await Promise.all([
      api("/api/debug/catalog"),
      api("/api/debug/lint"),
    ]);
    renderDebug(catalog, lint);
  } catch (err) {
    $("#debug-body").replaceChildren(el("p", { class: "error" }, String(err.message)));
  }
}

function renderDebug(catalog, lint) {
  const body = $("#debug-body");
  body.replaceChildren();

  // tools
  const tools = catalog.tools || [];
  body.append(el("div", { class: "section" },
    el("h3", {}, `工具（${tools.length}）`),
    el("table", {},
      el("thead", {}, el("tr", {}, el("th", {}, "名称"), el("th", {}, "入口"), el("th", {}, "说明"))),
      el("tbody", {}, tools.map((t) => {
        const callable = !!t.callable;
        return el("tr", {},
          el("td", { class: "mono" }, t.name),
          el("td", {}, el("span", { class: "badge " + (callable ? "ok" : "readonly") }, callable ? "可调" : "只读（仅供查看）")),
          el("td", { class: "muted" }, (t.description || "").slice(0, 90)),
        );
      })),
    ),
  ));

  // resources
  const kinds = catalog.resource_kinds || [];
  body.append(el("div", { class: "section" },
    el("h3", {}, `资源类型（${kinds.length}）`),
    el("table", {},
      el("thead", {}, el("tr", {}, el("th", {}, "URI"), el("th", {}, "读写"), el("th", {}, "说明"))),
      el("tbody", {}, kinds.map((r) => el("tr", {},
        el("td", { class: "mono" }, r.uri),
        el("td", {}, el("span", { class: "badge " + (r.writable ? "writable" : "readonly") }, r.writable ? "可写" : "只读")),
        el("td", { class: "muted" }, r.description),
      ))),
    ),
  ));

  // prompts
  const prompts = catalog.prompts || [];
  body.append(el("div", { class: "section" },
    el("h3", {}, `提示词（${prompts.length}）`),
    el("table", {},
      el("thead", {}, el("tr", {}, el("th", {}, "名称"), el("th", {}, "说明"))),
      el("tbody", {}, prompts.map((p) => el("tr", {},
        el("td", { class: "mono" }, p.name),
        el("td", { class: "muted" }, p.description),
      ))),
    ),
  ));

  // schema lint
  body.append(el("div", { class: "section" },
    el("h3", {}, `schema 可移植性检查（${lint.errors} error / ${lint.warnings} warning）`),
    (lint.issues || []).length
      ? el("table", {},
          el("thead", {}, el("tr", {}, el("th", {}, "规则"), el("th", {}, "严重度"), el("th", {}, "路径"), el("th", {}, "问题"))),
          el("tbody", {}, lint.issues.map((i) => el("tr", {},
            el("td", { class: "mono" }, i.rule),
            el("td", {}, el("span", { class: "badge " + (i.severity === "error" ? "readonly" : "warn") }, i.severity)),
            el("td", { class: "mono" }, i.path),
            el("td", { class: "muted" }, i.issue + " → " + i.suggestion),
          ))),
        )
      : el("p", { class: "muted" }, "无问题"),
  ));

  // tool caller
  const callableTools = tools.filter((t) => t.callable);
  body.append(el("div", { class: "section" },
    el("h3", {}, "调用工具"),
    el("div", { class: "toolbar" },
      el("select", { id: "debug-tool" }, callableTools.map((t) => el("option", { value: t.name }, t.name))),
      el("button", { class: "primary", onclick: callTool }, "调用"),
    ),
    el("textarea", { id: "debug-args", placeholder: '{\n  \n}' }, "{}"),
    el("div", { id: "debug-result" }),
  ));

  // raw JSON-RPC view
  body.append(el("div", { class: "section" },
    el("h3", {}, "协议报文（JSON-RPC 原文）"),
    el("pre", { id: "debug-raw", class: "mono" }, "发起一次调用后这里显示请求 / 响应原文。"),
  ));
}

async function callTool() {
  const name = $("#debug-tool").value;
  let args;
  try {
    args = JSON.parse($("#debug-args").value || "{}");
  } catch (err) {
    alert("参数不是合法 JSON：" + err.message);
    return;
  }
  const req = { jsonrpc: "2.0", id: debugId++, method: "tools/call", params: { name: name, arguments: args } };
  try {
    const data = await api("/api/debug", { method: "POST", body: req });
    renderRaw(data);
    renderToolResult(data.response && data.response.result);
  } catch (err) {
    renderRaw({ request: req, response: { error: { message: String(err.message) } } });
  }
}

function renderRaw(data) {
  $("#debug-raw").textContent =
    "请求 →\n" + pretty(data.request) + "\n\n响应 →\n" + pretty(data.response);
}

function renderToolResult(result) {
  const box = $("#debug-result");
  box.replaceChildren();
  if (!result) return;
  if (result.isError) {
    box.append(el("p", { class: "error" }, "工具返回错误"));
  }
  const content = result.content || [];
  for (const item of content) {
    if (item.type === "text") {
      box.append(el("pre", {}, item.text));
    } else if (item.type === "image") {
      box.append(el("img", { class: "preview", src: "data:" + item.mimeType + ";base64," + item.data }));
    } else {
      box.append(el("pre", {}, pretty(item)));
    }
  }
}

// ---- boot ----

(async function boot() {
  try {
    await api("/api/status");
    showApp();
  } catch (_) {
    showLogin();
  }
  setInterval(() => {
    if (!$("#app-view").classList.contains("hidden")) refreshStatus();
  }, 3000);
})();
