// Minimal Markdown renderer (§8.1): headings, paragraphs, inline code, code
// blocks, lists, blockquotes, links, tables and horizontal rules. It renders
// to a DOM tree with all raw HTML escaped first, so untrusted file content can
// never inject markup. No runtime dependency (fits the single-binary budget).

function esc(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function inline(text: string): string {
  let s = esc(text);
  // inline code first so its content is not further decorated
  s = s.replace(/`([^`]+)`/g, (_, c) => `<code>${c}</code>`);
  // links [text](url)
  s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_, txt, url) => {
    if (/^https?:\/\//i.test(url) || url.startsWith("/") || url.startsWith("#")) {
      return `<a href="${esc(url)}" rel="noopener">${txt}</a>`;
    }
    return txt; // disallow javascript: and other schemes
  });
  // bold / italic
  s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  s = s.replace(/\*([^*]+)\*/g, "<em>$1</em>");
  return s;
}

export function renderMarkdown(src: string): string {
  const lines = src.replace(/\r\n/g, "\n").split("\n");
  const html: string[] = [];
  let i = 0;
  let inCode = false;
  let codeBuf: string[] = [];
  let codeLang = "";

  const flushParagraph = (buf: string[]) => {
    if (buf.length) {
      html.push(`<p>${buf.map(inline).join("<br>")}</p>`);
      buf.length = 0;
    }
  };

  const para: string[] = [];
  const flushInline = () => flushParagraph(para);

  while (i < lines.length) {
    const line = lines[i];

    if (inCode) {
      if (/^\s*```/.test(line)) {
        html.push(`<pre><code${codeLang ? ` class="language-${esc(codeLang)}"` : ""}>${esc(codeBuf.join("\n"))}</code></pre>`);
        inCode = false;
        codeBuf = [];
      } else {
        codeBuf.push(line);
      }
      i++;
      continue;
    }

    const fence = line.match(/^\s*```\s*(\S*)/);
    if (fence) {
      flushInline();
      inCode = true;
      codeLang = fence[1];
      i++;
      continue;
    }

    if (/^\s*$/.test(line)) {
      flushInline();
      i++;
      continue;
    }

    const h = line.match(/^(#{1,6})\s+(.*)$/);
    if (h) {
      flushInline();
      const level = h[1].length;
      html.push(`<h${level}>${inline(h[2])}</h${level}>`);
      i++;
      continue;
    }

    if (/^\s*([-*_]\s*){3,}\s*$/.test(line)) {
      flushInline();
      html.push("<hr>");
      i++;
      continue;
    }

    const quote = line.match(/^\s*>\s?(.*)$/);
    if (quote) {
      flushInline();
      const buf: string[] = [];
      while (i < lines.length && /^\s*>\s?/.test(lines[i])) {
        buf.push(lines[i].replace(/^\s*>\s?/, ""));
        i++;
      }
      html.push(`<blockquote>${renderMarkdown(buf.join("\n"))}</blockquote>`);
      continue;
    }

    // tables
    if (line.startsWith("|") && i + 1 < lines.length && /^\s*\|?[\s:|-]+\|?\s*$/.test(lines[i + 1]) && lines[i + 1].includes("-")) {
      flushInline();
      const header = splitRow(line);
      const rows: string[][] = [header];
      i += 2;
      while (i < lines.length && lines[i].startsWith("|")) {
        rows.push(splitRow(lines[i]));
        i++;
      }
      html.push("<table><thead><tr>" + header.map((c) => `<th>${inline(c)}</th>`).join("") + "</tr></thead><tbody>" +
        rows.slice(1).map((r) => "<tr>" + r.map((c) => `<td>${inline(c)}</td>`).join("") + "</tr>").join("") +
        "</tbody></table>");
      continue;
    }

    const ul = line.match(/^\s*[-*+]\s+(.*)$/);
    const ol = line.match(/^\s*\d+\.\s+(.*)$/);
    if (ul || ol) {
      flushInline();
      const ordered = !!ol;
      const items: string[] = [];
      while (i < lines.length) {
        const m = ordered ? lines[i].match(/^\s*\d+\.\s+(.*)$/) : lines[i].match(/^\s*[-*+]\s+(.*)$/);
        if (!m) break;
        items.push(`<li>${inline(m[1])}</li>`);
        i++;
      }
      html.push(ordered ? `<ol>${items.join("")}</ol>` : `<ul>${items.join("")}</ul>`);
      continue;
    }

    para.push(line);
    i++;
  }
  if (inCode) {
    html.push(`<pre><code${codeLang ? ` class="language-${esc(codeLang)}"` : ""}>${esc(codeBuf.join("\n"))}</code></pre>`);
  }
  flushInline();
  return html.join("");
}

function splitRow(line: string): string[] {
  return line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((c) => c.trim());
}
