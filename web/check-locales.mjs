// Locale key parity check (§11.2): the zh-CN and en-US token tables must have
// identical key sets, or an English string is guaranteed to be missing. Run by
// the CI web job after `npm ci` and before the drift check.
import { readFileSync } from "node:fs";

const zh = JSON.parse(readFileSync(new URL("./locales/zh-CN.json", import.meta.url), "utf8"));
const en = JSON.parse(readFileSync(new URL("./locales/en-US.json", import.meta.url), "utf8"));

const zhKeys = Object.keys(zh);
const enKeys = Object.keys(en);
const onlyZh = zhKeys.filter((k) => !(k in en));
const onlyEn = enKeys.filter((k) => !(k in zh));

if (onlyZh.length || onlyEn.length) {
  console.error("locale key sets differ");
  if (onlyZh.length) console.error("only in zh-CN:", onlyZh.join(", "));
  if (onlyEn.length) console.error("only in en-US:", onlyEn.join(", "));
  process.exit(1);
}
console.log(`locale keys match (${zhKeys.length} keys)`);
