// All /api/* calls in one place, with a single 401 → login-page path (§7.1).

export class ApiError extends Error {
  status: number;
  detail?: string;
  constructor(status: number, message: string, detail?: string) {
    super(message);
    this.status = status;
    this.detail = detail;
  }
}

type UnauthorizedHandler = () => void;
let onUnauthorized: UnauthorizedHandler = () => {};

export function setUnauthorizedHandler(fn: UnauthorizedHandler): void {
  onUnauthorized = fn;
}

async function req(method: string, url: string, body?: unknown): Promise<any> {
  const opts: RequestInit = { method, credentials: "same-origin" };
  if (body instanceof FormData) {
    opts.body = body;
  } else if (body !== undefined) {
    opts.headers = { "Content-Type": "application/json" };
    opts.body = JSON.stringify(body);
  }
  const resp = await fetch(url, opts);
  if (resp.status === 401) {
    onUnauthorized();
    throw new ApiError(401, "unauthorized");
  }
  const text = await resp.text();
  let data: any = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = text;
  }
  if (!resp.ok) {
    const msg = typeof data === "string" ? data : data?.error ?? data?.message ?? (text || resp.statusText);
    throw new ApiError(resp.status, String(msg), String(msg));
  }
  return data;
}

export const api = {
  login(username: string, password: string): Promise<{ ok: boolean }> {
    return req("POST", "/api/login", { username, password });
  },
  logout(): Promise<{ ok: boolean }> {
    return req("POST", "/api/logout", {});
  },
  status(): Promise<Status> {
    return req("GET", "/api/status");
  },
  resources(): Promise<{ resources: ResourceEntry[] }> {
    return req("GET", "/api/resources");
  },
  read(uri: string): Promise<{ uri: string; mimeType: string; text?: string; blob?: string }> {
    return req("GET", `/api/resources/read?uri=${encodeURIComponent(uri)}`);
  },
  write(name: string, content: string): Promise<any> {
    return req("PUT", "/api/resources/file", { name, content });
  },
  upload(file: File, target: string): Promise<any> {
    const fd = new FormData();
    fd.append("file", file);
    if (target) fd.append("target", target);
    return req("POST", "/api/upload", fd);
  },
  debugCatalog(): Promise<any> {
    return req("GET", "/api/debug/catalog");
  },
  debugLint(): Promise<any> {
    return req("GET", "/api/debug/lint");
  },
  debug(raw: unknown): Promise<any> {
    return req("POST", "/api/debug", raw);
  },
};

// --- types (mirror the backend shapes) ---

export interface ResourceEntry {
  uri: string;
  name?: string;
  description?: string;
  mimeType?: string;
  size?: number;
  writable?: boolean;
  sha256?: string;
  mod_time?: string;
}

export interface Status {
  version: string;
  go: string;
  os: string;
  arch: string;
  android: { sessions: number; assets: string; assets_ok: boolean };
  ios: { sessions: number; scope: string };
  h5: { sessions: number };
  limits: { max_sessions: number; idle_timeout: string };
  sessions: {
    android: AndroidSession[];
    ios: string[];
    h5: H5Session[];
  };
  reclaimed: Reclaimed[];
}

export interface AndroidSession {
  session: string;
  pool: number;
  last_used: string;
  engines: { in_use: number; idle: number; created: number };
}

export interface H5Session {
  session: string;
  target: string;
  last_used: string;
  state: string;
}

export interface Reclaimed {
  session: string;
  backend: string;
  reason: string;
  at: string;
}

export interface ToolInfo {
  name: string;
  description: string;
  inputSchema: any;
  callable?: boolean;
}

// base64 (with possible padding) → Uint8Array
export function b64ToBytes(b64: string): Uint8Array<ArrayBuffer> {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function blobURL(b64: string, mime: string): string {
  const bytes = b64ToBytes(b64);
  const blob = new Blob([bytes], { type: mime || "application/octet-stream" });
  return URL.createObjectURL(blob);
}

// loadTools reads the runvm://tools catalog through resources/read, so the tool
// pages can never drift from what the MCP server actually advertises.
export async function loadTools(): Promise<ToolInfo[]> {
  const r = await api.read("runvm://tools");
  const parsed = JSON.parse(r.text || "{}");
  return (parsed.tools || []) as ToolInfo[];
}
