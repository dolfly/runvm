import "./styles/base.css";
import { el, mount } from "./dom";
import { api, setUnauthorizedHandler } from "./api";
import { lang, setLang, onLang, t } from "./i18n";
import { currentRoute, isResourcesRoute, go, onNavigate, start, Route } from "./router";
import { renderLogin } from "./pages/login";
import { renderOverview } from "./pages/overview";
import { renderSearch } from "./pages/search";
import { renderTools } from "./pages/tools";
import { renderScreenshots } from "./pages/screenshots";
import { renderFiles } from "./pages/files";
import { renderDebug } from "./pages/debug";

const app = document.getElementById("app")!;
// Remove the first-paint skeleton now that the real shell is about to mount.
document.getElementById("boot")?.remove();

let authed = false;
let version = "dev";

setUnauthorizedHandler(() => {
  if (authed) {
    authed = false;
    void render();
  }
});

function renderTopbar(route: Route): HTMLElement {
  const bar = el("div", { class: "topbar" });
  const brand = el("a", { class: "brand", href: "/", onclick: (e: Event) => { e.preventDefault(); go("/"); } });
  brand.appendChild(document.createTextNode("runvm"));
  brand.appendChild(el("span", { class: "ver" }, version));
  bar.appendChild(brand);

  const nav = el("nav", { class: "nav" });
  const items: [string, string, Route][] = [
    [t("nav.overview"), "/", "/"],
    [t("nav.resources"), "/resources", "/resources"],
    [t("nav.debug"), "/debug", "/debug"],
  ];
  for (const [label, href, r] of items) {
    const a = el("a", { href, class: route === r ? "active" : "" }, label);
    a.addEventListener("click", (e) => {
      e.preventDefault();
      go(href);
    });
    nav.appendChild(a);
  }
  bar.appendChild(nav);

  const right = el("div", { class: "row" });
  if (route === "/") {
    right.appendChild(el("span", { class: "dot ok", title: t("overview.auto_refresh") }));
    right.appendChild(el("span", { class: "mute" }, t("overview.auto_refresh")));
  }
  right.appendChild(renderLangSwitch());
  const logout = el("button", { class: "btn ghost sm", onclick: () => void doLogout() }, t("action.logout"));
  right.appendChild(logout);
  bar.appendChild(right);
  return bar;
}

function renderLangSwitch(): HTMLElement {
  const seg = el("div", { class: "segmented", title: t("a11y.lang_toggle") });
  const zh = el("button", { class: lang() === "zh-CN" ? "active" : "" }, "中文");
  const en = el("button", { class: lang() === "en" ? "active" : "" }, "EN");
  zh.addEventListener("click", () => setLang("zh-CN"));
  en.addEventListener("click", () => setLang("en"));
  seg.appendChild(zh);
  seg.appendChild(en);
  return seg;
}

function renderSidebar(route: Route): HTMLElement {
  const side = el("div", { class: "sidebar" });
  const items: [string, string, Route][] = [
    ["⌕", t("nav.search"), "/resources"],
    ["▦", t("nav.tools"), "/resources/tools"],
    ["▣", t("nav.screenshots"), "/resources/screenshots"],
    ["▤", t("nav.files"), "/resources/files"],
  ];
  for (const [icon, label, r] of items) {
    const a = el("a", { class: "sidebar-item" + (route === r ? " active" : ""), href: r });
    a.appendChild(el("span", { class: "ic" }, icon));
    a.appendChild(document.createTextNode(label));
    a.addEventListener("click", (e) => {
      e.preventDefault();
      go(r);
    });
    side.appendChild(a);
  }
  side.appendChild(el("div", { class: "sidebar-root mono", title: "runvm://files" }, `${t("meta.resource_root")}: runvm://files`));
  return side;
}

function renderPage(route: Route): HTMLElement {
  switch (route) {
    case "/": return renderOverview();
    case "/resources": return renderSearch();
    case "/resources/tools": return renderTools();
    case "/resources/screenshots": return renderScreenshots();
    case "/resources/files": return renderFiles();
    case "/debug": return renderDebug();
  }
}

async function render(): Promise<void> {
  if (!authed) {
    mount(app, renderLogin(() => {}));
    return;
  }
  const route = currentRoute();
  const shell = el("div", { class: "app-shell" });
  shell.appendChild(renderTopbar(route));
  const row = el("div", { class: "app-row" });
  if (isResourcesRoute(route)) row.appendChild(renderSidebar(route));
  const content = el("div", { class: "app-content" });
  content.appendChild(renderPage(route));
  row.appendChild(content);
  shell.appendChild(row);
  mount(app, shell);
}

async function doLogout() {
  try {
    await api.logout();
  } finally {
    authed = false;
    go("/");
    void render();
  }
}

async function boot() {
  try {
    const st = await api.status();
    authed = true;
    version = st.version;
  } catch {
    authed = false;
  }
  await render();
}

onNavigate(() => void render());
onLang(() => void render());
start();
void boot();
