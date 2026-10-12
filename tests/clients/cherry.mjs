// Test a downloaded, unmodified Cherry Studio release through its real UI.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import http from 'node:http';
import net from 'node:net';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium, expect } from '@playwright/test';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const evidence = path.join(root, '.cache/cherry-evidence');
mkdirSync(evidence, { recursive: true });
const key = process.env.FAKEAPI_CLIENT_KEY;
assert(key && process.env.CHERRY_EXECUTABLE && process.env.FAKEAPI_TEST_URL);
const redact = value => String(value).replaceAll(key, '<redacted>');
const passed = [];
const errors = [];
const knownWarnings = [];
const requests = [];
const cases = [
  { id: 'chat', endpoint: 'openai-chat-completions', suffix: '/v1', route: '/v1/chat/completions' },
  { id: 'responses', endpoint: 'openai-responses', suffix: '/v1', route: '/v1/responses' },
  { id: 'anthropic', endpoint: 'anthropic-messages', suffix: '/anthropic/v1', route: '/anthropic/v1/messages' },
  { id: 'gemini', endpoint: 'google-generate-content', suffix: '/v1beta', route: '/v1beta/models/fakeapi-test:streamGenerateContent' }
];
const started = Date.now();
let app, browser, page, failure;

function pass(label) { passed.push(label); console.log('PASS', label); }
async function port() {
  const server = net.createServer();
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const value = server.address().port;
  await new Promise(resolve => server.close(resolve));
  return value;
}

// Observe method/path and the numeric output-limit control to verify the wire.
// Headers, messages, query credentials, and responses are never recorded.
const proxy = http.createServer((incoming, outgoing) => {
  const target = new URL(incoming.url, process.env.FAKEAPI_TEST_URL);
  const record = { method: incoming.method, path: target.pathname };
  requests.push(record);
  // Retain only the typed numeric output-limit control, never message content.
  let size = 0;
  const fragments = [];
  incoming.on('data', chunk => { size += chunk.length; if (size <= 65536) fragments.push(chunk); });
  incoming.on('end', () => {
    if (size > 65536 || incoming.method !== 'POST') return;
    try {
      const body = JSON.parse(Buffer.concat(fragments).toString());
      const value = body.max_tokens;
      record.maxTokens = { present: Object.hasOwn(body, 'max_tokens'), type: value === null ? 'null' : typeof value };
      if (typeof value === 'number') record.maxTokens.value = value;
    } catch {}
  });
  const forwarded = http.request(target, { method: incoming.method, headers: incoming.headers }, response => {
    outgoing.writeHead(response.statusCode, response.headers);
    response.on('error', () => outgoing.destroy());
    response.pipe(outgoing);
  });
  forwarded.on('error', () => { if (!outgoing.headersSent) outgoing.writeHead(502); outgoing.end(); });
  outgoing.on('close', () => { if (!outgoing.writableFinished) forwarded.destroy(); });
  incoming.pipe(forwarded);
});

async function dataApi(method, route, body) {
  const response = await page.evaluate(async ({ method, route, body }) =>
    window.api.dataApi.request({ id: crypto.randomUUID(), method, path: route, ...(body === undefined ? {} : { body }) }),
  { method, route, body });
  if (response.error) throw new Error(`Cherry Data API ${method} ${route}: ${redact(JSON.stringify(response.error))}`);
  return response.data;
}

async function select(providerId, modelId) {
  const button = page.getByRole('button', { name: 'Selected models', exact: true });
  await button.click();
  const picker = page.locator('[data-testid="model-selector-content"]:visible').last();
  await expect(picker).toBeVisible();
  await picker.getByTestId('model-selector-search').fill(modelId);
  const option = picker.locator(`[role="option"][data-testid$="${providerId}::${modelId}"]`).first();
  await expect(option).toBeVisible();
  await option.click();
  await page.keyboard.press('Escape');
}

async function send(prompt, expected, code = false) {
  const messages = page.locator('[data-ui~="chat.message"][data-message-id]:visible');
  const before = await messages.evaluateAll(nodes => nodes.map(node => node.getAttribute('data-message-id')));
  await page.locator('[data-ui~="chat.composer"] [contenteditable="true"]').first().fill(prompt);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const exclusions = before.map(id => `:not([data-message-id="${id}"])`).join('');
  const answer = page.locator(`[data-ui~="chat.message"][data-message-id]${exclusions}:visible`)
    .filter({ has: page.locator('.message-assistant') }).last();
  await expect(answer).toBeVisible({ timeout: 45000 });
  if (code) {
    await expect.poll(async () => (await answer.locator('.code-viewer .line-content')
      .evaluateAll(lines => lines.map(line => line.textContent).join('\n'))).trimEnd(), { timeout: 45000 })
      .toBe(expected.trimEnd());
  } else {
    await expect(answer.getByText(expected, { exact: true }).last()).toBeVisible({ timeout: 45000 });
  }
  const id = await answer.getAttribute('data-message-id');
  await expect.poll(async () => (await dataApi('GET', `/messages/${id}`))?.status, { timeout: 45000 }).toBe('success');
  return answer;
}

try {
  await new Promise(resolve => proxy.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${proxy.address().port}`;
  const cdp = await port();
  const profile = path.join(root, '.cache/cherry-profile');
  mkdirSync(profile, { recursive: true });
  app = spawn(process.env.CHERRY_EXECUTABLE, ['--no-sandbox', '--disable-gpu', `--remote-debugging-port=${cdp}`,
    `--user-data-dir=${profile}/chromium`], {
    env: { ...process.env, XDG_CONFIG_HOME: profile, XDG_CACHE_HOME: `${profile}/cache`, LANG: 'en_US.UTF-8' },
    stdio: ['ignore', 'pipe', 'pipe']
  });
  let startup = '';
  for (const stream of [app.stdout, app.stderr]) stream.on('data', chunk => { startup = (startup + redact(chunk)).slice(-6000); });
  app.on('error', error => { startup += redact(error.message); });
  for (let attempt = 0; attempt < 120; attempt++) {
    try { browser = await chromium.connectOverCDP(`http://127.0.0.1:${cdp}`, { timeout: 1000 }); break; }
    catch { await new Promise(resolve => setTimeout(resolve, 500)); }
  }
  if (!browser) throw new Error(`Cherry Studio CDP startup failed: ${startup}`);
  await expect.poll(() => browser.contexts().flatMap(context => context.pages())
    .some(candidate => candidate.url().includes('/windows/main/index.html')), { timeout: 60000 }).toBe(true);
  page = browser.contexts().flatMap(context => context.pages()).find(candidate => candidate.url().includes('/windows/main/index.html'));
  page.on('pageerror', error => errors.push(redact(error.message)));
  page.on('console', message => {
    if (message.type() !== 'error') return;
    const text = redact(message.text());
    // Cherry 2.1.6's fallback avatar label logs newly-created provider IDs as
    // missing translations. Record that client warning separately from errors.
    if (/\[i18n:label\] Missing key|\[I18N\] Missing key:/.test(text)
      && cases.some(item => item.providerId && text.includes(item.providerId))) knownWarnings.push(text);
    else errors.push(text);
  });
  await page.setViewportSize({ width: 1280, height: 900 });
  await expect(page.locator('#root')).toBeVisible({ timeout: 60000 });
  assert.match(await page.title(), /Cherry Studio/);
  await page.evaluate(() => window.api.preference.setMultiple({
    'app.language': 'en-US', 'app.onboarding.provider_setup.status': 'skipped',
    'app.privacy.data_collection.enabled': false, 'feature.quick_assistant.enabled': false,
    'feature.selection.enabled': false
  }));
  await page.reload();
  await expect(page.locator('[data-ui="app.shell"]').first()).toBeVisible({ timeout: 60000 });
  await page.keyboard.press('Escape');
  pass('Cherry Studio 2.1.6: main window identity, nonblank app, and isolated configuration');

  for (const item of cases) {
    // Cherry Studio recognizes UUID provider IDs as user-created providers.
    const providerId = crypto.randomUUID();
    item.providerId = providerId;
    await dataApi('POST', '/providers', {
      providerId, name: `FakeAPI ${item.id}`, endpointConfigs: { [item.endpoint]: { baseUrl: base + item.suffix } },
      defaultChatEndpoint: item.endpoint, authConfig: { type: 'api-key' },
      apiKeys: [{ id: crypto.randomUUID(), key, isEnabled: true }]
    });
    await dataApi('PATCH', `/providers/${providerId}`, { isEnabled: true });
    await dataApi('POST', '/models', ['fakeapi-test', 'fakeapi-frog'].map(modelId => ({
      providerId, modelId, name: `${item.id} ${modelId}`, group: 'FakeAPI',
      endpointTypes: [item.endpoint], inputModalities: ['text'], outputModalities: ['text'], supportsStreaming: true,
      contextWindow: 32768, maxInputTokens: 32768, maxOutputTokens: 8192
    })));
  }
  await page.reload();
  await expect(page.locator('[data-ui="app.shell"]').first()).toBeVisible({ timeout: 60000 });
  await page.getByRole('button', { name: 'Chat', exact: true }).first().click();

  for (const item of cases) {
    const before = requests.length;
    await select(item.providerId, 'fakeapi-test');
    await send('只回答 OK', 'OK');
    await send('1+1', '2');
    assert(requests.slice(before).some(request => request.method === 'POST' && request.path === item.route),
      `${item.id}: client did not use its configured protocol`);
    await page.screenshot({ path: path.join(evidence, `${item.id}.png`) });
    pass(`${item.id}: real client stream, completed message, two-turn history, and native wire route`);
  }

  await select(cases[0].providerId, 'fakeapi-frog');
  const laugh = readFileSync(path.join(process.env.FAKEAPI_TEST_ASSETS, 'nailong/laugh.txt'), 'utf8');
  const drawing = await send('/laugh', laugh, true);
  pass('ASCII material preserved exactly in the rendered code block');
  await drawing.locator('.code-viewer').last().screenshot({ path: path.join(evidence, 'ascii-laugh.png') });

  await page.locator('[data-ui~="chat.composer"] [contenteditable="true"]').first().fill('/client/cancel');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const stop = page.getByRole('button', { name: /^Stop/ }).last();
  await expect(stop).toBeVisible({ timeout: 20000 });
  await stop.click();
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible({ timeout: 10000 });
  await select(cases[0].providerId, 'fakeapi-test');
  await send('1+1', '2');
  await page.screenshot({ path: path.join(evidence, 'cancel-recovery.png') });
  pass('Stop generation releases the single request slot and allows a new reply');
  assert.equal(await page.locator('vite-error-overlay, #webpack-dev-server-client-overlay').count(), 0);
  assert.equal(errors.length, 0, `renderer errors: ${errors.join('\n').slice(0,1500)}`);
  pass('no renderer exceptions, unexpected console errors, or framework overlay in the tested flow');
} catch (error) {
  failure = redact(error.stack ?? error);
  if (page) {
    await page.screenshot({ path: path.join(evidence, 'failure.png') }).catch(() => {});
    const visible = await page.locator('body').innerText().catch(() => '');
    writeFileSync(path.join(evidence, 'failure-ui.txt'), redact(visible).slice(-15000));
  }
  console.error('FAIL', failure);
} finally {
  writeFileSync(path.join(evidence, 'report.json'), JSON.stringify({ version: '2.1.6',
    browser: 'Browser plugin not available; Playwright CDP in isolated GitHub runner', viewport: [1280,900],
    passed, success: !failure, failure, rendererErrors: errors, knownWarnings, requests,
    durationSeconds: Math.round((Date.now() - started) / 1000) }, null, 2));
  if (browser) await browser.close().catch(() => {});
  if (app && app.exitCode === null) app.kill('SIGTERM');
  await new Promise(resolve => proxy.close(resolve));
}
if (failure) process.exitCode = 1;
