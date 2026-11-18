#!/usr/bin/env node
/**
 * Drive the WebBrain extension in a real Chromium through the webbrain MCP
 * server: ask-mode analysis and act-mode interaction on a target page.
 *
 * Env:
 *   WEBBRAIN_EXT        path to built extension (build/chrome)
 *   WEBBRAIN_MCP        path to webbrain mcp-server dist/index.js
 *   ANTHROPIC_BASE_URL  e.g. https://ark.cn-beijing.volces.com/api/coding
 *   ANTHROPIC_API_KEY
 *   ANTHROPIC_MODEL     e.g. doubao-seed-2-1-pro-260628
 *   TARGET_URL          default https://www.xiaohongshu.com
 *   CHROMIUM_PATH       optional playwright chromium executable override
 *   PAIRING_KEY         optional (default: ~/.webbrain/bridge-key)
 *
 * Requires: playwright in the webbrain checkout (npm ci in the repo root).
 */
const { chromium } = require('playwright');
const { spawn } = require('child_process');
const readline = require('readline');
const fs = require('fs');
const os = require('os');
const path = require('path');

const EXT = process.env.WEBBRAIN_EXT;
const MCP = process.env.WEBBRAIN_MCP;
const BASE = process.env.ANTHROPIC_BASE_URL;
const KEY = process.env.ANTHROPIC_API_KEY;
const MODEL = process.env.ANTHROPIC_MODEL || 'doubao-seed-2-1-pro-260628';
const TARGET = process.env.TARGET_URL || 'https://www.xiaohongshu.com';
const PROFILE = fs.mkdtempSync(path.join(os.tmpdir(), 'wb-profile-'));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

for (const [k, v] of [['WEBBRAIN_EXT', EXT], ['WEBBRAIN_MCP', MCP], ['ANTHROPIC_BASE_URL', BASE], ['ANTHROPIC_API_KEY', KEY]]) {
  if (!v) { console.error(`missing env ${k}`); process.exit(2); }
}

class McpClient {
  constructor(proc) {
    this.proc = proc;
    this.pending = new Map();
    this.nextId = 1;
    const rl = readline.createInterface({ input: proc.stdout });
    rl.on('line', (line) => {
      if (!line.trim()) return;
      let msg;
      try { msg = JSON.parse(line); } catch { return; }
      if (msg.id != null && this.pending.has(msg.id)) {
        const { resolve, reject } = this.pending.get(msg.id);
        this.pending.delete(msg.id);
        msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result);
      }
    });
  }
  call(method, params = {}) {
    const id = this.nextId++;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.proc.stdin.write(JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n');
    });
  }
  notify(method, params = {}) {
    this.proc.stdin.write(JSON.stringify({ jsonrpc: '2.0', method, params }) + '\n');
  }
}

async function main() {
  const pairingKey = process.env.PAIRING_KEY || fs.readFileSync(path.join(os.homedir(), '.webbrain', 'bridge-key'), 'utf8').trim();

  // 1. browser + extension
  const launchOpts = {
    headless: false,
    args: [`--load-extension=${EXT}`, `--disable-extensions-except=${EXT}`, '--no-first-run', '--window-size=1440,900'],
    viewport: { width: 1440, height: 900 },
  };
  if (process.env.CHROMIUM_PATH) launchOpts.executablePath = process.env.CHROMIUM_PATH;
  const ctx = await chromium.launchPersistentContext(PROFILE, launchOpts);
  let sw = null;
  for (let i = 0; i < 40 && !sw; i++) {
    sw = ctx.serviceWorkers().find((s) => s.url().includes('background.js')) || null;
    if (!sw) await sleep(500);
  }
  if (!sw) throw new Error('extension service worker not found');
  const extId = new URL(sw.url()).host;
  console.log('[1] SW:', sw.url());

  // 2. inject provider + bridge config
  await sw.evaluate(async ({ base, key, model, pairing }) => {
    const cfg = { type: 'anthropic', label: 'Example Anthropic-compatible', category: 'cloud', baseUrl: base, apiKey: key, model, requiresApiKey: true, supportsTools: true, supportsVision: false, promptTier: 'full', contextWindow: 200000, configured: true };
    const data = await chrome.storage.local.get(['providers', 'activeProvider']);
    await chrome.storage.local.set({ providers: { ...(data.providers || {}), 'example-anthropic': cfg }, activeProvider: 'example-anthropic' });
    await chrome.storage.local.set({ webbrainCloudBridgeEnabled: true, webbrainCloudBridgeUrl: 'ws://127.0.0.1:17374/extension', webbrainCloudBridgePairingKey: pairing });
    return 'ok';
  }, { base: BASE, key: KEY, model: MODEL, pairing: pairingKey });
  console.log('[2] config injected');

  // 3. MCP server first, then bridge dial-out
  const mcp = spawn('node', [MCP], { stdio: ['pipe', 'pipe', 'pipe'] });
  mcp.stderr.on('data', (d) => { const s = String(d).trim(); if (s && /pair|listen|error/i.test(s)) console.log('[mcp]', s.slice(0, 160)); });
  const client = new McpClient(mcp);
  await client.call('initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'runvm-webbrain-example', version: '1.0.0' } });
  client.notify('notifications/initialized');
  await sleep(1500);

  const ui = await ctx.newPage();
  await ui.goto(`chrome-extension://${extId}/src/ui/settings.html`, { timeout: 30000 }).catch(() => {});
  await sleep(5000);
  await ui.evaluate(async () => {
    return await new Promise((resolve) => {
      chrome.runtime.sendMessage({ target: 'background', action: 'cloud_bridge_start', url: 'ws://127.0.0.1:17374/extension' }, (resp) => resolve(resp));
    });
  });
  let connected = false;
  for (let i = 0; i < 30 && !connected; i++) {
    connected = await ui.evaluate(async () => {
      return await new Promise((resolve) => {
        chrome.runtime.sendMessage({ target: 'background', action: 'cloud_bridge_status' }, (resp) => resolve(!!(resp && resp.connected)));
      });
    }).catch(() => false);
    if (!connected) await sleep(1000);
  }
  console.log('[3] bridge connected:', connected);
  await ui.close().catch(() => {});

  // 4. open the target page
  const page = await ctx.newPage();
  console.log('[4] opening', TARGET);
  await page.goto(TARGET, { waitUntil: 'domcontentloaded', timeout: 90000 }).catch(() => {});
  await sleep(8000);
  console.log('[4] url:', page.url(), '| title:', await page.title().catch(() => 'n/a'));

  // 5. ask-mode run
  const ask = await client.call('tools/call', {
    name: 'webbrain_run',
    arguments: { task: '请读取当前页面的内容并总结：页面标题、当前栏目、主要区域、能看到哪些内容。', mode: 'ask', timeout_seconds: 420, wait: true },
  });
  console.log('[5] ASK:\n' + (ask.content || []).map((c) => c.text || '').join('\n').slice(0, 2000));

  // 6. act-mode run (scroll twice)
  const act = await client.call('tools/call', {
    name: 'webbrain_run',
    arguments: { task: '在当前页面向下滚动两次，加载更多内容，然后告诉我新增了哪些内容', mode: 'act', timeout_seconds: 420, wait: true },
  });
  console.log('[6] ACT:\n' + (act.content || []).map((c) => c.text || '').join('\n').slice(0, 2000));

  await page.screenshot({ path: 'webbrain-final.png' }).catch(() => {});
  console.log('[7] screenshot: webbrain-final.png');
  mcp.kill('SIGTERM');
  await ctx.close().catch(() => {});
  console.log('[8] done');
  process.exit(0);
}

main().catch((e) => { console.error('FATAL', e); try { process.exit(1); } catch {} });
