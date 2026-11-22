// Formatting helpers (§11.6). Timestamps are always ISO 8601 UTC and never
// localized; sizes use binary units; counts go through the i18n table.

const UNITS = ["B", "KiB", "MiB", "GiB", "TiB"];

export function fmtBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "—";
  if (n === 0) return "0 B";
  let i = 0;
  let v = n;
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  const s = v < 10 && i > 0 ? v.toFixed(1) : Math.round(v).toString();
  return `${s} ${UNITS[i]}`;
}

export function fmtCount(n: number): string {
  if (!Number.isFinite(n)) return "—";
  return n >= 1000 ? n.toLocaleString("en-US") : String(n);
}

export function fmtTimeShort(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const now = new Date();
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  const hh = String(d.getUTCHours()).padStart(2, "0");
  const mm = String(d.getUTCMinutes()).padStart(2, "0");
  if (sameDay) return `${hh}:${mm}`;
  const MM = String(d.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(d.getUTCDate()).padStart(2, "0");
  return `${MM}-${dd} ${hh}:${mm}`;
}

export function fmtRelative(iso: string, lang: string): string {
  const d = new Date(iso);
  const ms = Date.now() - d.getTime();
  const rtf = new Intl.RelativeTimeFormat(lang, { numeric: "auto" });
  const s = Math.round(ms / 1000);
  if (s < 60) return rtf.format(-s, "second");
  const m = Math.round(s / 60);
  if (m < 60) return rtf.format(-m, "minute");
  const h = Math.round(m / 60);
  if (h < 24) return rtf.format(-h, "hour");
  const days = Math.round(h / 24);
  return rtf.format(-days, "day");
}

export function shaPrefix(sha: string): string {
  return sha ? sha.slice(0, 8) : "";
}

export function fmtMs(n: number): string {
  return `${Math.round(n)}ms`;
}
