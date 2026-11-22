import { el, clear, append } from "../dom";
import { api, ResourceEntry, blobURL } from "../api";
import { t, tn } from "../i18n";
import { fmtBytes, fmtTimeShort, shaPrefix } from "../format";
import { go } from "../router";
import { jsonTree, emptyState, errorState, pill, toast, confirmDialog } from "../ui";
import { renderMarkdown } from "../markdown";

type Kind = "image" | "json" | "md" | "text" | "other";

interface Tree {
  name: string;
  full: string;
  dir: boolean;
  children: Tree[];
}

export function renderFiles(): HTMLElement {
  const root = el("div", { class: "files-layout" });

  // three panes
  const treePane = el("div", { class: "files-tree-pane" });
  const listPane = el("div", { class: "files-list-pane" });
  const viewPane = el("div", { class: "files-view-pane" });
  root.appendChild(treePane);
  root.appendChild(listPane);
  root.appendChild(viewPane);

  // tree pane header + body
  treePane.appendChild(el("div", { class: "editor-bar" }, el("span", { class: "mute" }, "runvm://files")));
  const treeEl = el("div", { class: "file-tree" });
  treePane.appendChild(treeEl);

  // list pane toolbar + table
  const listBar = el("div", { class: "editor-bar" });
  const upBtn = el("button", { class: "btn icon sm", title: ".." }, "↑");
  const crumb = el("div", { class: "files-crumb mono" });
  const uploadBtn = el("button", { class: "btn primary sm" }, "↑ " + t("action.upload"));
  const fileInput = el("input", { type: "file", multiple: "true", style: "display:none" });
  listBar.appendChild(upBtn);
  listBar.appendChild(crumb);
  listBar.appendChild(el("span", { class: "spacer" }));
  listBar.appendChild(uploadBtn);
  listBar.appendChild(fileInput);
  listPane.appendChild(listBar);
  const listBody = el("div", { class: "files-list-body" });
  listPane.appendChild(listBody);

  // view pane
  const viewBar = el("div", { class: "editor-bar" });
  viewPane.appendChild(viewBar);
  const viewBody = el("div", { class: "files-view-body" });
  viewPane.appendChild(viewBody);
  const statusBar = el("div", { class: "statusbar mono" });
  viewPane.appendChild(statusBar);

  let files: ResourceEntry[] = [];
  let cwd = new URLSearchParams(window.location.search).get("path") || "";
  let selected: string | null = new URLSearchParams(window.location.search).get("file");
  let editing = false;
  let dirty = false;
  let editContent = "";
  let editKind: Kind = "text";
  let statePill: HTMLElement | null = null;

  // --- data / tree ---

  function buildTree(): Tree[] {
    const roots: Tree[] = [];
    const ensure = (nodes: Tree[], seg: string, full: string, dir: boolean): Tree => {
      let n = nodes.find((x) => x.name === seg && x.dir === dir);
      if (!n) {
        n = { name: seg, full, dir, children: [] };
        nodes.push(n);
      }
      return n;
    };
    for (const f of files) {
      const name = f.name || "";
      const segs = name.split("/");
      let cur = roots;
      let acc = "";
      segs.forEach((seg, i) => {
        acc = acc ? `${acc}/${seg}` : seg;
        const isLast = i === segs.length - 1;
        if (isLast) ensure(cur, seg, acc, false);
        else {
          const d = ensure(cur, seg, acc, true);
          cur = d.children;
        }
      });
    }
    const sortNodes = (nodes: Tree[]) => {
      nodes.sort((a, b) => (a.dir === b.dir ? a.name.localeCompare(b.name) : a.dir ? -1 : 1));
      for (const n of nodes) if (n.dir) sortNodes(n.children);
    };
    sortNodes(roots);
    return roots;
  }

  function renderTree() {
    treeEl.replaceChildren();
    // fixed read-only entry: the tool catalog lives on the tools page (§8.3)
    const toolsRow = el("div", { class: "tree-node readonly" });
    toolsRow.appendChild(el("span", { class: "tree-ic" }, "▦"));
    toolsRow.appendChild(el("span", { class: "tree-name mono" }, t("files.tools_entry")));
    toolsRow.appendChild(pill("outline", t("state.readonly")));
    toolsRow.addEventListener("click", () => go("/resources/tools"));
    treeEl.appendChild(toolsRow);

    const roots = buildTree();
    for (const n of roots) treeEl.appendChild(renderTreeNode(n, 0));
  }

  function renderTreeNode(n: Tree, depth: number): HTMLElement {
    const holder = el("div");
    const node = el("div", { class: "tree-node" + (selected === n.full ? " selected" : "") });
    node.style.paddingLeft = `${depth * 12 + 8}px`;
    const arrow = el("span", { class: "tree-arrow" }, n.dir ? "▾" : "");
    node.appendChild(arrow);
    node.appendChild(el("span", { class: "tree-name mono" }, n.name));
    node.addEventListener("click", () => {
      if (n.dir) setCwd(n.full);
      else select(n.full);
    });
    holder.appendChild(node);
    if (n.dir) {
      let open = true;
      const childWrap = el("div");
      for (const c of n.children) childWrap.appendChild(renderTreeNode(c, depth + 1));
      arrow.addEventListener("click", (e) => {
        e.stopPropagation();
        open = !open;
        arrow.textContent = open ? "▾" : "▸";
        childWrap.style.display = open ? "" : "none";
      });
      holder.appendChild(childWrap);
    }
    return holder;
  }

  function setCwd(dir: string) {
    cwd = dir;
    go(`/resources/files?path=${encodeURIComponent(dir)}`);
    renderList();
    renderCrumb();
  }

  function select(name: string) {
    selected = name;
    editing = false;
    dirty = false;
    go(`/resources/files?path=${encodeURIComponent(cwd)}&file=${encodeURIComponent(name)}`);
    renderTree();
    renderList();
    void renderView();
  }

  function renderCrumb() {
    clear(crumb);
    const segs = cwd ? cwd.split("/") : [];
    const rootLink = el("span", { class: "crumb-seg" }, "runvm://files");
    rootLink.addEventListener("click", () => setCwd(""));
    crumb.appendChild(rootLink);
    let acc = "";
    for (const s of segs) {
      acc = acc ? `${acc}/${s}` : s;
      crumb.appendChild(el("span", { class: "crumb-slash" }, "/"));
      const seg = el("span", { class: "crumb-seg mono" }, s);
      const target = acc;
      seg.addEventListener("click", () => setCwd(target));
      crumb.appendChild(seg);
    }
  }

  function directChildren(): { dirs: string[]; files: ResourceEntry[] } {
    const dirs = new Set<string>();
    const direct: ResourceEntry[] = [];
    for (const f of files) {
      const name = f.name || "";
      const rel = cwd ? (name.startsWith(cwd + "/") ? name.slice(cwd.length + 1) : null) : name;
      if (rel === null) continue;
      const i = rel.indexOf("/");
      if (i >= 0) {
        dirs.add(rel.slice(0, i));
      } else if (rel) {
        direct.push(f);
      }
    }
    return { dirs: [...dirs].sort(), files: direct };
  }

  function renderList() {
    listBody.replaceChildren();
    const { dirs, files: direct } = directChildren();
    if (!dirs.length && !direct.length) {
      listBody.appendChild(emptyState("▤", t("empty.files.title"), t("empty.files.desc"),
        el("button", { class: "btn ghost sm", onclick: () => fileInput.click() }, t("empty.upload_action"))));
      return;
    }
    const table = el("table", { class: "table" });
    table.appendChild(el("thead", {}, el("tr", {},
      el("th", {}, t("table.col.name")), el("th", { class: "num" }, t("table.col.size")), el("th", {}, t("table.col.modified")),
    )));
    const tbody = el("tbody");
    for (const d of dirs) {
      const row = el("tr", {});
      row.appendChild(el("td", { class: "mono" }, "▸ " + d + "/"));
      row.appendChild(el("td", { class: "num" }, "—"));
      row.appendChild(el("td", {}, "—"));
      row.style.cursor = "pointer";
      row.addEventListener("click", () => setCwd(cwd ? `${cwd}/${d}` : d));
      tbody.appendChild(row);
    }
    for (const f of direct) {
      const name = f.name || "";
      const row = el("tr", { class: selected === name ? "selected" : "" });
      row.appendChild(el("td", { class: "mono" }, name.split("/").pop() || name));
      row.appendChild(el("td", { class: "num" }, fmtBytes(f.size || 0)));
      row.appendChild(el("td", { class: "mono", title: f.mod_time || "" }, f.mod_time ? fmtTimeShort(f.mod_time) : "—"));
      row.style.cursor = "pointer";
      row.addEventListener("click", () => select(name));
      tbody.appendChild(row);
    }
    table.appendChild(tbody);
    listBody.appendChild(table);
    listBody.appendChild(el("div", { class: "files-count mono" }, tn("unit.items", dirs.length + direct.length)));
  }

  // --- view / edit ---

  function kindOf(entry: ResourceEntry): Kind {
    const name = entry.name || "";
    const mime = entry.mimeType || "";
    const ext = name.slice(name.lastIndexOf(".") + 1).toLowerCase();
    const IMG = ["png", "jpg", "jpeg", "gif", "webp", "bmp", "svg"];
    const TEXT = ["txt", "log", "yaml", "yml", "toml", "csv", "ini", "conf", "go", "js", "css", "html", "sh"];
    if (mime.startsWith("image/") || IMG.includes(ext)) return "image";
    if (mime === "application/json" || ext === "json") return "json";
    if (ext === "md" || ext === "markdown") return "md";
    if (mime.startsWith("text/") || TEXT.includes(ext)) return "text";
    return "other";
  }

  async function renderView() {
    clear(viewBar);
    clear(viewBody);
    clear(statusBar);
    if (!selected) {
      viewBody.appendChild(emptyState("▤", t("empty.files.title"), t("empty.files.desc")));
      return;
    }
    const entry = files.find((f) => f.name === selected);
    if (!entry) {
      viewBody.appendChild(errorState(t("error.not_found"), selected));
      return;
    }
    const kind = kindOf(entry);
    editKind = kind;

    // header bar
    viewBar.appendChild(el("span", { class: "mono" }, entry.name || ""));
    viewBar.appendChild(pill("neutral", extLabel(entry), { mono: true }));
    const writable = entry.writable !== false;
    statePill = pill(writable ? "ok" : "outline", writable ? t("state.writable") : t("state.readonly"), { dot: writable ? "ok" : undefined });
    viewBar.appendChild(statePill);
    viewBar.appendChild(el("span", { class: "spacer" }));

    if (!writable) {
      // read-only resource: no edit affordance at all (§8.3)
      viewBar.appendChild(el("span", { class: "mute" }, t("files.readonly_desc")));
    } else if (!editing && kind !== "other" && kind !== "image") {
      viewBar.appendChild(el("button", { class: "btn ghost sm", onclick: () => enterEdit() }, t("action.edit")));
    }
    if (kind === "image" && !editing) {
      viewBar.appendChild(el("button", { class: "btn ghost sm", onclick: () => download(entry) }, t("action.download")));
    } else if (kind === "other") {
      viewBar.appendChild(el("button", { class: "btn ghost sm", onclick: () => download(entry) }, t("action.download")));
    }

    if (editing) {
      viewBar.appendChild(el("button", { class: "btn primary sm", onclick: () => void save() }, t("action.save")));
      viewBar.appendChild(el("button", { class: "btn ghost sm", onclick: () => void discard() }, t("action.discard")));
    }

    try {
      const r = await api.read(entry.uri);
      if (kind === "image") {
        if (r.blob) {
          const url = blobURL(r.blob, r.mimeType || "image/png");
          const holder = el("div", { class: "img-preview" });
          holder.appendChild(el("img", { src: url, decoding: "async" }));
          viewBody.appendChild(holder);
        }
      } else if (kind === "other") {
        if (r.blob) {
          viewBody.appendChild(binaryInfo(entry));
        }
      } else {
        const text = r.text ?? "";
        if (editing) {
          renderEditor(text);
        } else if (kind === "json") {
          let parsed: unknown;
          try {
            parsed = JSON.parse(text);
            viewBody.appendChild(jsonTree(parsed));
          } catch {
            viewBody.appendChild(errorState(t("error.invalid_json"), ""));
          }
        } else if (kind === "md") {
          const prose = el("div", { class: "prose" });
          prose.innerHTML = renderMarkdown(text);
          viewBody.appendChild(prose);
        } else {
          const pre = el("pre", { class: "codeblock" }, text);
          viewBody.appendChild(pre);
        }
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      viewBody.appendChild(errorState(t("error.load_failed"), msg));
    }

    clear(statusBar);
    append(statusBar,
      el("span", {}, extLabel(entry)),
      el("span", {}, fmtBytes(entry.size || 0)),
      entry.mod_time ? el("span", { title: entry.mod_time }, entry.mod_time) : null,
      entry.sha256 ? el("span", {}, `sha256 ${shaPrefix(entry.sha256)}…`) : null,
      el("span", { class: "spacer" }),
      writable ? pill("ok", t("state.writable"), { dot: "ok" }) : pill("outline", t("state.readonly")),
    );
  }

  function extLabel(entry: ResourceEntry): string {
    const name = entry.name || "";
    const i = name.lastIndexOf(".");
    return i >= 0 ? name.slice(i + 1).toLowerCase() : "file";
  }

  function binaryInfo(entry: ResourceEntry): HTMLElement {
    const box = el("div", { class: "binary-info" });
    const rows: [string, string][] = [
      [t("table.col.size"), fmtBytes(entry.size || 0)],
      [t("table.col.type"), entry.mimeType || "—"],
      [t("meta.modified"), entry.mod_time || "—"],
      [t("meta.sha256"), entry.sha256 || "—"],
    ];
    for (const [k, v] of rows) {
      box.appendChild(el("div", { class: "binary-row" },
        el("span", { class: "mute" }, k), el("span", { class: "mono" }, v)));
    }
    return box;
  }

  function enterEdit() {
    editing = true;
    dirty = false;
    void renderView();
  }

  function renderEditor(text: string) {
    editContent = text;
    const ta = el("textarea", { class: "textarea mono files-editor", spellcheck: "false" });
    ta.value = text;
    ta.addEventListener("input", () => {
      editContent = ta.value;
      if (!dirty) {
        dirty = true;
        if (statePill) {
          statePill.className = "pill warn";
          statePill.textContent = t("state.dirty");
        }
      }
    });
    viewBody.appendChild(ta);
  }

  async function save() {
    const entry = files.find((f) => f.name === selected);
    if (!entry) return;
    if (editKind === "json") {
      const err = validateJson(editContent);
      if (err) {
        viewBody.prepend(errorState(t("error.invalid_json"), err));
        return;
      }
    }
    try {
      await api.write(selected!, editContent);
      editing = false;
      dirty = false;
      toast("ok", t("state.saved"));
      await reload();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      viewBody.prepend(errorState(t("error.save_failed"), msg));
    }
  }

  async function discard() {
    const ok = await confirmDialog(t("files.discard_confirm_title"), t("files.lose_edits"), { confirmText: t("action.discard"), danger: true });
    if (!ok) return;
    editing = false;
    dirty = false;
    await renderView();
  }

  async function download(entry: ResourceEntry) {
    try {
      const r = await api.read(entry.uri);
      if (r.blob) {
        const url = blobURL(r.blob, r.mimeType || "application/octet-stream");
        const a = el("a", { href: url, download: (entry.name || "").split("/").pop() || "file" }) as HTMLAnchorElement;
        a.click();
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      toast("danger", t("error.load_failed"), msg);
    }
  }

  async function reload() {
    const res = await api.resources();
    files = (res.resources || []).filter((r) => r.uri.startsWith("runvm://files/"));
    renderTree();
    renderList();
    renderCrumb();
    await renderView();
  }

  // --- upload ---

  uploadBtn.addEventListener("click", () => fileInput.click());
  fileInput.addEventListener("change", async () => {
    const list = Array.from(fileInput.files || []);
    fileInput.value = "";
    for (const f of list) {
      const target = cwd ? `${cwd}/${f.name}` : f.name;
      try {
        await api.upload(f, target);
        toast("ok", t("state.saved"), `runvm://files/${target}`);
      } catch (e) {
        const msg = e instanceof Error ? e.message : String(e);
        toast("danger", t("error.upload_failed"), msg);
      }
    }
    await reload();
  });

  // drag & drop onto the file list
  listBody.addEventListener("dragover", (e) => {
    e.preventDefault();
    listBody.classList.add("drag-over");
  });
  listBody.addEventListener("dragleave", () => listBody.classList.remove("drag-over"));
  listBody.addEventListener("drop", async (e) => {
    e.preventDefault();
    listBody.classList.remove("drag-over");
    const items = Array.from(e.dataTransfer?.files || []);
    for (const f of items) {
      const target = cwd ? `${cwd}/${f.name}` : f.name;
      try {
        await api.upload(f, target);
        toast("ok", t("state.saved"), `runvm://files/${target}`);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        toast("danger", t("error.upload_failed"), msg);
      }
    }
    await reload();
  });

  upBtn.addEventListener("click", () => {
    if (!cwd) return;
    const i = cwd.lastIndexOf("/");
    setCwd(i >= 0 ? cwd.slice(0, i) : "");
  });

  // --- init ---

  const timer = setInterval(() => {
    if (!document.body.contains(root)) clearInterval(timer);
  }, 1000);

  void (async () => {
    try {
      const res = await api.resources();
      files = (res.resources || []).filter((r) => r.uri.startsWith("runvm://files/"));
      renderTree();
      renderCrumb();
      renderList();
      await renderView();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      viewBody.replaceChildren(errorState(t("error.load_failed"), msg));
    }
  })();

  return root;
}

function validateJson(text: string): string | null {
  try {
    JSON.parse(text);
    return null;
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    const m = msg.match(/position\s+(\d+)/i);
    if (m) {
      const pos = Number(m[1]);
      const before = text.slice(0, pos);
      const line = before.split("\n").length;
      const col = pos - (before.lastIndexOf("\n") + 1) + 1;
      return `${t("error.json_line", { line, col })} · ${msg}`;
    }
    return msg;
  }
}
