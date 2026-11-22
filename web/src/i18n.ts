// Lightweight i18n (§11). No framework, no ICU: a flat token table per
// language, `{name}` interpolation, `.one`/`.other` plural pairs, and a
// `data-i18n*` scan for static markup. Missing keys warn loudly and return the
// key itself so an untranslated string is visible in development.

import zh from "../locales/zh-CN.json";
import en from "../locales/en-US.json";

type Dict = Record<string, string>;
const locales: Record<string, Dict> = { "zh-CN": zh as Dict, en: en as Dict };

export type Lang = "zh-CN" | "en";

let current: Lang = detect();

function detect(): Lang {
  const url = new URLSearchParams(window.location.search).get("lang");
  if (url === "zh-CN" || url === "en") {
    persistOnly(url);
    const q = new URLSearchParams(window.location.search);
    q.delete("lang");
    const s = q.toString();
    history.replaceState(null, "", window.location.pathname + (s ? `?${s}` : ""));
    return url;
  }
  const stored = localStorage.getItem("runvm.lang");
  if (stored === "zh-CN" || stored === "en") return stored;
  const cookie = readCookie("runvm_lang");
  if (cookie === "zh-CN" || cookie === "en") return cookie;
  const nav = (navigator.languages?.[0] ?? navigator.language ?? "").toLowerCase();
  if (nav.startsWith("zh")) return "zh-CN";
  if (nav.startsWith("en")) return "en";
  return "zh-CN";
}

function persistOnly(l: Lang): void {
  localStorage.setItem("runvm.lang", l);
  document.cookie = `runvm_lang=${l}; path=/; max-age=31536000; SameSite=Lax`;
}

function readCookie(name: string): string | null {
  const m = document.cookie.match(new RegExp(`(?:^|;\\s*)${name}=([^;]+)`));
  return m ? decodeURIComponent(m[1]) : null;
}

export function lang(): Lang {
  return current;
}

export function setLang(l: Lang): void {
  if (l === current) return;
  current = l;
  persistOnly(l);
  document.documentElement.lang = l;
  document.documentElement.dataset.lang = l;
  for (const fn of listeners) fn(l);
}

const listeners: ((l: Lang) => void)[] = [];
export function onLang(fn: (l: Lang) => void): void {
  listeners.push(fn);
}

export function t(key: string, vars: Record<string, string | number> = {}): string {
  let s = locales[current][key];
  if (s == null) {
    console.warn(`[i18n] missing key: ${key} (${current})`);
    s = key;
  }
  for (const [k, v] of Object.entries(vars)) {
    s = s.split(`{${k}}`).join(String(v));
  }
  return s;
}

export function tn(base: string, n: number): string {
  return t(`${base}.${n === 1 ? "one" : "other"}`, { n });
}

export function applyI18n(root: Element): void {
  root.querySelectorAll<HTMLElement>("[data-i18n]").forEach((elm) => {
    elm.textContent = t(elm.getAttribute("data-i18n")!);
  });
  root.querySelectorAll<HTMLElement>("[data-i18n-title]").forEach((elm) => {
    elm.setAttribute("title", t(elm.getAttribute("data-i18n-title")!));
  });
  root.querySelectorAll<HTMLInputElement>("[data-i18n-ph]").forEach((elm) => {
    elm.placeholder = t(elm.getAttribute("data-i18n-ph")!);
  });
}
