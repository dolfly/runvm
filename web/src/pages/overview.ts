import { el } from "../dom";
import { api, Status } from "../api";
import { t } from "../i18n";
import { fmtCount } from "../format";
import { pill, errorState, dialog } from "../ui";

export function renderOverview(): HTMLElement {
  const root = el("div", { class: "page" });
  const header = el("div", { class: "page-header" });
  const titleRow = el("div", { class: "row" });
  titleRow.appendChild(el("h1", { class: "page-title" }, t("page.overview.title")));
  titleRow.appendChild(el("span", { class: "spacer" }));
  const autoRef = el("button", { class: "btn ghost sm", title: t("overview.auto_refresh") });
  const liveDot = el("span", { class: "dot ok" });
  autoRef.appendChild(liveDot);
  autoRef.appendChild(document.createTextNode(t("overview.auto_refresh")));
  const refreshBtn = el("button", { class: "btn ghost sm", onclick: () => void load() }, "↻ " + t("action.refresh"));
  titleRow.appendChild(autoRef);
  titleRow.appendChild(refreshBtn);
  header.appendChild(titleRow);
  header.appendChild(el("div", { class: "page-sub" }, t("page.overview.subtitle")));
  root.appendChild(header);

  const body = el("div", { class: "overview-body" });
  root.appendChild(body);

  let broken = 0;

  async function load() {
    try {
      const st = await api.status();
      broken = 0;
      liveDot.className = "dot ok";
      draw(st);
    } catch (e) {
      broken++;
      liveDot.className = "dot danger";
      if (broken >= 3) {
        clearInterval(timer);
      }
      const msg = e instanceof Error ? e.message : String(e);
      if (body.childElementCount === 0 || broken === 1) {
        body.replaceChildren(errorState(t("error.load_failed"), msg, () => void load()));
      }
    }
  }

  function draw(st: Status) {
    body.replaceChildren();

    const backendsTitle = el("div", { class: "section-title" }, t("page.overview.backends"));
    const backendGrid = el("div", { class: "card-grid" });
    backendGrid.appendChild(backendCard("android", st.android.sessions, st.android.assets_ok, st.android.assets));
    backendGrid.appendChild(backendCard("ios", st.ios.sessions, true, st.ios.scope));
    backendGrid.appendChild(backendCard("h5", st.h5.sessions, true, "—"));
    body.appendChild(backendsTitle);
    body.appendChild(backendGrid);

    const limitsTitle = el("div", { class: "section-title" }, t("page.overview.limits"));
    const limitGrid = el("div", { class: "card-grid" });
    limitGrid.appendChild(limitCard(t("meta.session_limit"), st.limits.max_sessions === 0 ? t("meta.unlimited") : fmtCount(st.limits.max_sessions)));
    limitGrid.appendChild(limitCard(t("meta.idle_timeout"), st.limits.idle_timeout));
    const reclaimed = st.reclaimed?.length ?? 0;
    const reclaimedCard = limitCard(t("meta.reclaimed"), fmtCount(reclaimed));
    if (reclaimed > 0) {
      const link = el("button", { class: "btn link", onclick: () => showReclaimed(st.reclaimed) }, t("action.view_reclaimed"));
      reclaimedCard.appendChild(link);
    }
    limitGrid.appendChild(reclaimedCard);
    body.appendChild(limitsTitle);
    body.appendChild(limitGrid);

    const sessionsTitle = el("div", { class: "section-title row" });
    sessionsTitle.appendChild(document.createTextNode(t("page.overview.sessions")));
    sessionsTitle.appendChild(el("span", { class: "spacer" }));
    const filterSel = el("select", { class: "select sm" });
    for (const [v, label] of [
      ["all", t("overview.filter_all")],
      ["android", "android"],
      ["ios", "ios"],
      ["h5", "h5"],
    ] as [string, string][]) {
      const o = el("option", { value: v }, label) as HTMLOptionElement;
      if (v === filter) o.selected = true;
      filterSel.appendChild(o);
    }
    filterSel.addEventListener("change", () => {
      filter = filterSel.value;
      body.replaceChildren();
      void load();
    });
    sessionsTitle.appendChild(filterSel);
    const sessionsTable = buildSessionsTable(st);
    body.appendChild(sessionsTitle);
    body.appendChild(sessionsTable);

    // version footer
    body.appendChild(el("div", { class: "page-foot mono" }, `runvm ${st.version} · go${st.go} ${st.os}/${st.arch}`));
  }

  function backendCard(name: string, sessions: number, ok: boolean, note: string): HTMLElement {
    const card = el("div", { class: "card backend-card" });
    const top = el("div", { class: "row" });
    top.appendChild(el("span", { class: "backend-name mono" }, name));
    top.appendChild(el("span", { class: "spacer" }));
    if (ok) top.appendChild(pill("ok", t("state.available"), { dot: "ok" }));
    else top.appendChild(pill("outline", t("state.not_built"), { dot: "off" }));
    card.appendChild(top);
    card.appendChild(el("div", { class: "backend-count mono" }, fmtCount(sessions)));
    card.appendChild(el("div", { class: "backend-note", title: note }, note || "—"));
    const link = el("button", { class: "btn link sm", onclick: () => filterBy(name) }, t("action.view_sessions") + " →");
    card.appendChild(el("div", { class: "backend-link" }, link));
    return card;
  }

  function limitCard(label: string, value: string): HTMLElement {
    const card = el("div", { class: "card" });
    card.appendChild(el("div", { class: "meta" }, label));
    card.appendChild(el("div", { class: "limit-value mono" }, value));
    return card;
  }

  function buildSessionsTable(st: Status): HTMLElement {
    const rows: { backend: string; session: string; pool: string; engines: string; target: string }[] = [];
    for (const a of st.sessions.android) {
      rows.push({ backend: "android", session: a.session, pool: fmtCount(a.pool), engines: `${a.engines.in_use} / ${a.engines.idle} / ${a.engines.created}`, target: "—" });
    }
    for (const id of st.sessions.ios) {
      rows.push({ backend: "ios", session: id, pool: "—", engines: "—", target: "—" });
    }
    for (const h of st.sessions.h5) {
      rows.push({ backend: "h5", session: h.session, pool: "—", engines: "—", target: h.target });
    }
    const shown = rows.filter((r) => filter === "all" || r.backend === filter);

    if (shown.length === 0) {
      const empty = el("div", { class: "state" }, el("div", { class: "title" }, t("empty.sessions.title")));
      return empty;
    }

    const table = el("table", { class: "table" });
    const thead = el("thead", {}, el("tr", {},
      el("th", {}, t("table.col.backend")),
      el("th", {}, t("table.col.session")),
      el("th", {}, t("table.col.target")),
      el("th", {}, t("table.col.pool")),
      el("th", {}, t("table.col.engines")),
    ));
    const tbody = el("tbody");
    for (const r of shown) {
      tbody.appendChild(el("tr", {},
        el("td", { class: "mono" }, r.backend),
        el("td", { class: "mono" }, r.session),
        el("td", { class: "mono", title: r.target }, r.target),
        el("td", { class: "num" }, r.pool),
        el("td", { class: "mono" }, r.engines),
      ));
    }
    table.appendChild(thead);
    table.appendChild(tbody);
    return table;
  }

  function filterBy(b: string) {
    filter = b;
    body.replaceChildren();
    void load();
  }

  function showReclaimed(list: Status["reclaimed"]) {
    const table = el("table", { class: "table" });
    table.appendChild(el("thead", {}, el("tr", {},
      el("th", {}, t("table.col.backend")), el("th", {}, t("table.col.session")),
      el("th", {}, "reason"), el("th", {}, "at"),
    )));
    const tbody = el("tbody");
    for (const r of list) {
      tbody.appendChild(el("tr", {},
        el("td", { class: "mono" }, r.backend),
        el("td", { class: "mono" }, r.session),
        el("td", { class: "mono" }, r.reason),
        el("td", { class: "mono" }, r.at),
      ));
    }
    table.appendChild(tbody);
    dialog(t("meta.reclaimed"), table, [el("button", { class: "btn ghost", onclick: () => {} }, t("action.close"))]);
  }

  let filter = "all";
  const timer = setInterval(() => {
    if (!document.body.contains(root)) {
      clearInterval(timer);
      return;
    }
    void load();
  }, 3000);

  void load();
  return root;
}
