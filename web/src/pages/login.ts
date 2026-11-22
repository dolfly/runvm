import { el } from "../dom";
import { api, ApiError } from "../api";
import { t } from "../i18n";

export function renderLogin(onSuccess: () => void): HTMLElement {
  // DESIGN §7.1: after login, return to the path the user was trying to reach.
  // A `?next=` param (if a future server-side redirect adds one) wins; otherwise
  // fall back to the current deep-link path, so visiting /resources/tools while
  // logged out lands you back on /resources/tools after signing in.
  const next = nextPath();

  const wrap = el("div", { class: "login-wrap" });
  const col = el("div", { class: "login-col" });
  col.appendChild(el("div", { class: "login-brand mono" }, "runvm"));
  col.appendChild(el("div", { class: "login-sub" }, `${t("page.login.subtitle")} · Console`));

  const user = el("input", { class: "input", type: "text", autocomplete: "username", spellcheck: "false" });
  user.placeholder = t("field.username");
  const pass = el("input", { class: "input", type: "password", autocomplete: "current-password" });
  pass.placeholder = t("field.password");

  const err = el("div", { class: "login-err", role: "alert" });
  err.style.display = "none";

  const btn = el("button", { class: "btn primary login-btn" }, t("action.login"));

  const field = (label: string, input: HTMLElement): HTMLElement =>
    el("div", { class: "login-field" }, el("label", { class: "login-label" }, label), input);

  const submit = async () => {
    btn.disabled = true;
    user.disabled = true;
    pass.disabled = true;
    err.style.display = "none";
    try {
      await api.login(user.value, pass.value);
      onSuccess();
      history.replaceState(null, "", next);
      window.location.href = next; // full reload so the app boots authed on the deep link
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : t("error.network");
      err.textContent = msg;
      err.style.display = "block";
      btn.disabled = false;
      user.disabled = false;
      pass.disabled = false;
      pass.focus();
    }
  };

  btn.addEventListener("click", submit);
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Enter") {
      e.preventDefault();
      submit();
    }
  };
  user.addEventListener("keydown", onKey);
  pass.addEventListener("keydown", onKey);

  col.appendChild(field(t("field.username"), user));
  col.appendChild(field(t("field.password"), pass));
  col.appendChild(btn);
  col.appendChild(err);
  col.appendChild(el("div", { class: "login-version mono" }, "runvm"));
  wrap.appendChild(col);
  return wrap;
}

// nextPath resolves where to send the user after a successful login (§7.1).
function nextPath(): string {
  const explicit = new URLSearchParams(window.location.search).get("next");
  if (explicit) return explicit;
  const current = window.location.pathname + window.location.search;
  if (current && current !== "/") return current;
  return "/";
}
