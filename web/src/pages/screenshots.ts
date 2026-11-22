import { el } from "../dom";
import { api, blobURL } from "../api";
import { t } from "../i18n";
import { fmtBytes } from "../format";
import { go } from "../router";
import { emptyState, errorState } from "../ui";

interface Shot {
  uri: string;
  session: string;
  target: string;
}

export function renderScreenshots(): HTMLElement {
  const root = el("div", { class: "shots-layout" });
  const listPane = el("div", { class: "shots-list" });
  const previewPane = el("div", { class: "shots-preview" });

  const toolbar = el("div", { class: "editor-bar" });
  const search = el("div", { class: "search" });
  const input = el("input", { class: "input", type: "text" });
  input.placeholder = t("field.search_session");
  search.appendChild(el("span", { class: "ic" }, "⌕"));
  search.appendChild(input);
  toolbar.appendChild(search);
  listPane.appendChild(toolbar);
  const listEl = el("div", { class: "shots-list-body" });
  listPane.appendChild(listEl);

  root.appendChild(listPane);
  root.appendChild(previewPane);

  let shots: Shot[] = [];
  let selected: string | null = new URLSearchParams(window.location.search).get("session");

  async function load() {
    try {
      const res = await api.resources();
      shots = (res.resources || [])
        .filter((r) => r.uri.startsWith("runvm://screenshot/"))
        .map((r) => ({ uri: r.uri, session: r.uri.slice("runvm://screenshot/".length), target: r.description || "" }));
      if (!selected && shots.length) selected = shots[0].session;
      drawList();
      drawPreview();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      previewPane.replaceChildren(errorState(t("error.load_failed"), msg, () => void load()));
    }
  }

  function drawList() {
    listEl.replaceChildren();
    const q = input.value.trim().toLowerCase();
    const hits = shots.filter((s) => s.session.toLowerCase().includes(q) || s.target.toLowerCase().includes(q));
    if (!hits.length) {
      listEl.appendChild(emptyState("▣", t("empty.screenshots.title"), t("empty.screenshots.desc")));
    }
    for (const s of hits) {
      const row = el("div", { class: "shot-row" + (s.session === selected ? " selected" : "") });
      row.appendChild(el("div", { class: "shot-row-main" },
        el("div", { class: "shot-row-name mono" }, s.session + ".png"),
        el("div", { class: "shot-row-desc" }, s.target),
      ));
      row.addEventListener("click", () => {
        selected = s.session;
        go(`/resources/screenshots?session=${encodeURIComponent(s.session)}`);
        drawList();
        void drawPreview();
      });
      listEl.appendChild(row);
    }
  }

  async function drawPreview() {
    previewPane.replaceChildren();
    const s = shots.find((x) => x.session === selected);
    if (!s) {
      previewPane.appendChild(emptyState("▣", t("empty.screenshots.title"), ""));
      return;
    }
    const bar = el("div", { class: "editor-bar" });
    bar.appendChild(el("span", { class: "mono" }, s.session + ".png"));
    bar.appendChild(el("span", { class: "spacer" }));
    const openBtn = el("button", { class: "btn ghost sm", disabled: "true" }, t("action.open_original"));
    const dlBtn = el("button", { class: "btn ghost sm", disabled: "true" }, t("action.download"));
    bar.appendChild(openBtn);
    bar.appendChild(dlBtn);
    previewPane.appendChild(bar);

    const holder = el("div", { class: "img-preview" });
    holder.appendChild(el("div", { class: "mute" }, t("state.loading")));
    previewPane.appendChild(holder);

    const info = el("div", { class: "shots-info mono" });
    previewPane.appendChild(info);

    try {
      const r = await api.read(s.uri);
      if (!r.blob) throw new Error(t("error.not_found"));
      const bytes = r.blob;
      const url = blobURL(bytes, r.mimeType);
      const dims = pngSize(bytes);
      const img = el("img", { src: url, decoding: "async" });
      holder.replaceChildren(img);
      openBtn.removeAttribute("disabled");
      dlBtn.removeAttribute("disabled");
      openBtn.addEventListener("click", () => window.open(url, "_blank", "noopener"));
      dlBtn.addEventListener("click", () => {
        const a = el("a", { href: url, download: s.session + ".png" }) as HTMLAnchorElement;
        a.click();
      });
      const size = b64Length(bytes);
      info.textContent = `${dims ? `${dims.w}×${dims.h} · ` : ""}${fmtBytes(size)} · PNG`;
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      holder.replaceChildren(errorState(t("error.load_failed"), msg));
    }
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

  input.addEventListener("input", () => drawList());

  void load();
  return root;
}

function b64Length(b64: string): number {
  let n = b64.length;
  let pad = 0;
  if (b64.endsWith("==")) pad = 2;
  else if (b64.endsWith("=")) pad = 1;
  return Math.floor((n * 3) / 4) - pad;
}

function pngSize(b64: string): { w: number; h: number } | null {
  const bytes = new Uint8Array(
    atob(b64).split("").map((c) => c.charCodeAt(0)),
  );
  if (bytes.length < 24) return null;
  const w = (bytes[16] << 24) | (bytes[17] << 16) | (bytes[18] << 8) | bytes[19];
  const h = (bytes[20] << 24) | (bytes[21] << 16) | (bytes[22] << 8) | bytes[23];
  return { w, h };
}
