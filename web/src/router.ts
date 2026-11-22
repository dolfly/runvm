// Minimal history.pushState router. Pages render into #app via main.ts; links
// and page code call go() to switch routes without a full reload (§6.3).

export type Route = "/" | "/resources" | "/resources/tools" | "/resources/screenshots" | "/resources/files" | "/debug";

const listeners: (() => void)[] = [];

export function currentRoute(): Route {
  const p = window.location.pathname;
  if (p === "/" || p === "") return "/";
  if (p === "/resources") return "/resources";
  if (p === "/resources/tools") return "/resources/tools";
  if (p === "/resources/screenshots") return "/resources/screenshots";
  if (p === "/resources/files") return "/resources/files";
  if (p === "/debug") return "/debug";
  return "/";
}

export function isResourcesRoute(r: Route): boolean {
  return r.startsWith("/resources");
}

// go() navigates to a path (with optional query) and re-renders.
export function go(path: string): void {
  history.pushState(null, "", path);
  notify();
}

export function replace(path: string): void {
  history.replaceState(null, "", path);
  notify();
}

export function onNavigate(fn: () => void): void {
  listeners.push(fn);
}

function notify(): void {
  for (const fn of listeners) fn();
}

export function start(): void {
  window.addEventListener("popstate", notify);
}
