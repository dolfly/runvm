// Small, safe DOM helpers. The one hard lesson from DO-130 lives here: every
// child that is an array is flattened before it is appended, so a table row or
// a dropdown can never render as "[object HTMLTableRowElement]".
export type Child = Node | string | null | undefined | false | Child[];

export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, unknown> = {},
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v == null || v === false) continue;
    if (k === "class") node.className = String(v);
    else if (k === "dataset") Object.assign(node.dataset, v as Record<string, string>);
    else if (k.startsWith("on") && typeof v === "function") {
      node.addEventListener(k.slice(2).toLowerCase(), v as EventListener);
    } else if (k === "value") (node as unknown as HTMLInputElement).value = String(v);
    else if (k === "checked") (node as unknown as HTMLInputElement).checked = Boolean(v);
    else node.setAttribute(k, String(v));
  }
  append(node, ...children);
  return node;
}

export function append(parent: Node, ...children: Child[]): void {
  for (const child of flatten(children)) {
    if (child == null || child === false) continue;
    parent.appendChild(child instanceof Node ? child : document.createTextNode(String(child)));
  }
}

export function clear(node: Node): void {
  while (node.firstChild) node.removeChild(node.firstChild);
}

export function mount(root: HTMLElement, node: Node): void {
  clear(root);
  root.appendChild(node);
}

function flatten(children: Child[]): Child[] {
  const out: Child[] = [];
  for (const c of children) {
    if (Array.isArray(c)) out.push(...flatten(c));
    else out.push(c);
  }
  return out;
}

export function frag(...children: Child[]): DocumentFragment {
  const f = document.createDocumentFragment();
  append(f, ...children);
  return f;
}

export function svgIcon(path: string, size = 14): SVGSVGElement {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("width", String(size));
  svg.setAttribute("height", String(size));
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", "2");
  const p = document.createElementNS("http://www.w3.org/2000/svg", "path");
  p.setAttribute("d", path);
  p.setAttribute("stroke-linecap", "round");
  p.setAttribute("stroke-linejoin", "round");
  svg.appendChild(p);
  return svg;
}
