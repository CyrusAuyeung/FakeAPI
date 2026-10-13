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
const showcase = process.env.FAKEAPI_SHOWCASE === '1';
const recordStream = process.env.FAKEAPI_RECORD_STREAM === '1';
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

// Observe public transport state and fixture labels to verify the wire.
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
      if (Array.isArray(body.messages)) {
        record.reasoningHistoryPresent = body.messages.some(message =>
          message.role === 'assistant' && typeof message.reasoning_content === 'string' && message.reasoning_content.length > 0);
        const latest = body.messages.filter(message => message.role === 'user').at(-1)?.content;
        const text = typeof latest === 'string' ? latest : Array.isArray(latest)
          ? latest.filter(part => part.type === 'text').map(part => part.text).join('\n') : '';
        record.fixture = new Map([['只回答 OK', 'ok'], ['1+1', 'math'], ['/laugh', 'laugh'], ['/client/cancel', 'cancel'], ['/client/native', 'native']]).get(text) ?? 'other';
      }
    } catch {}
  });
  let upstreamResponse;
  const forwarded = http.request(target, { method: incoming.method, headers: incoming.headers }, response => {
    upstreamResponse = response;
    record.status = response.statusCode;
    record.responseBytes = 0;
    let eventBuffer = '';
    const decoder = new TextDecoder();
    response.on('data', chunk => {
      record.responseBytes += chunk.length;
      if (showcase || !String(response.headers['content-type']).includes('text/event-stream')) return;
      eventBuffer += decoder.decode(chunk, { stream: true });
      let newline;
      while ((newline = eventBuffer.indexOf('\n')) >= 0) {
        const line = eventBuffer.slice(0, newline).trimEnd();
        eventBuffer = eventBuffer.slice(newline + 1);
        if (!line.startsWith('data: ') || line === 'data: [DONE]') continue;
        try {
          const event = JSON.parse(line.slice(6));
          for (const choice of event.choices ?? []) {
            const delta = choice.delta ?? {};
            if (typeof delta.reasoning_content === 'string' && delta.reasoning_content.length) record.nativeReasoningSeen = true;
            if (typeof delta.content === 'string' && delta.content.includes('[模拟思考]')) record.simulatedThinkingInBody = true;
          }
        } catch {}
      }
      if (eventBuffer.length > 1048576) eventBuffer = '';
    });
    response.on('close', () => { record.upstreamClosed = true; });
    outgoing.writeHead(response.statusCode, response.headers);
    response.on('error', () => outgoing.destroy());
    response.pipe(outgoing);
  });
  forwarded.on('error', () => { if (!outgoing.headersSent) outgoing.writeHead(502); outgoing.end(); });
  outgoing.on('close', () => {
    if (!outgoing.writableFinished) {
      record.clientAborted = true;
      upstreamResponse?.destroy();
      forwarded.destroy();
    }
  });
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
  await expect(button).toContainText(modelId);
}

async function send(prompt, expected, code = false) {
  const messages = page.locator('[data-ui~="chat.message"][data-message-id]:visible');
  const before = await messages.evaluateAll(nodes => nodes.map(node => node.getAttribute('data-message-id')));
  const composer = page.locator('[data-ui~="chat.composer"] [contenteditable="true"]').first();
  await composer.fill('');
  await composer.pressSequentially(prompt, { delay: 20 });
  await expect(composer).toHaveText(prompt);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const exclusions = before.map(id => `:not([data-message-id="${id}"])`).join('');
  const newMessages = page.locator(`[data-ui~="chat.message"][data-message-id]${exclusions}:visible`);
  // Cherry virtualizes old bubbles when a large answer scrolls to the bottom.
  // Verify the submitted fixture on the wire, rather than requiring that bubble
  // to remain mounted after the answer has already arrived.
  const answer = newMessages.filter({ has: page.locator('.message-assistant') }).last();
  await expect(answer).toBeVisible({ timeout: 45000 });
  if (code) {
    const allowed = (Array.isArray(expected) ? expected : [expected]).map(text => text.trimEnd());
    await expect.poll(async () => allowed.includes((await answer.locator('.code-viewer .line-content')
      .evaluateAll(lines => lines.map(line => line.textContent).join('\n'))).trimEnd()), { timeout: 45000 })
      .toBe(true);
  } else {
    await expect(answer.getByText(expected, { exact: true }).last()).toBeVisible({ timeout: 45000 });
  }
  const id = await answer.getAttribute('data-message-id');
  await expect.poll(async () => (await dataApi('GET', `/messages/${id}`))?.status, { timeout: 45000 }).toBe('success');
  return answer;
}

async function recordStreaming() {
  const viewport = { width: 1280, height: 1500 };
  const clip = { x: 326, y: 44, width: 946, height: 1448 };
  await page.setViewportSize(viewport);
  await page.evaluate(() => window.api.preference.setMultiple({
    'ui.theme_mode': 'dark', 'chat.message.thought.auto_collapse': false
  }));
  await page.reload();
  await expect(page.locator('[data-ui="app.shell"]').first()).toBeVisible({ timeout: 60000 });
  await select(cases[0].providerId, 'fakeapi-frog');
  const prompt = '帮我写一个快速排序';
  const expected = readFileSync(path.join(process.env.FAKEAPI_TEST_ASSETS, 'nailong/laugh.txt'), 'utf8').trimEnd();
  const thinking = '[模拟思考] ' + readFileSync(path.join(process.env.FAKEAPI_TEST_ASSETS, 'thinking/natural.txt'), 'utf8').trimEnd();
  const composer = page.locator('[data-ui~="chat.composer"] [contenteditable="true"]').first();
  await composer.fill(prompt);
  await expect(composer).toHaveText(prompt);
  await page.waitForTimeout(500);

  const directory = path.join(evidence, 'frames');
  mkdirSync(directory, { recursive: true });
  const frames = [];
  const startedAt = performance.now();
  let recording = true, frameFailure;
  const capture = (async () => {
    try {
      while (recording && performance.now() - startedAt < 45000) {
        const timestamp = performance.now() - startedAt;
        const file = `frame-${String(frames.length).padStart(5, '0')}.png`;
        await page.screenshot({ path: path.join(directory, file), clip });
        const body = await page.locator('.code-viewer .line-content')
          .evaluateAll(lines => lines.map(line => line.textContent).join('\n'));
        const thinkingVisible = await page.locator('[data-testid="live-tool-group-content"], [data-testid="tool-history-content"]')
          .getByText(thinking, { exact: true }).last().isVisible().catch(() => false);
        const state = await page.locator('[data-testid="live-tool-group"], [data-testid="completed-process-trigger"]').evaluateAll(nodes => {
          const node = nodes.at(-1);
          return node ? { visible: node.getBoundingClientRect().height > 0,
            expanded: node.getAttribute('data-testid') === 'live-tool-group' || node.getAttribute('aria-expanded') === 'true' } : null;
        });
        frames.push({ file, timestampMs: Math.round(timestamp), codeChars: body.trimEnd().length, thinkingVisible,
          reasoningPanelVisible: !!state?.visible, reasoningExpanded: state?.expanded ?? null });
        await page.waitForTimeout(Math.max(0, 125 - (performance.now() - startedAt - timestamp)));
      }
    } catch (error) { frameFailure = error; }
  })();
  try {
    await page.waitForTimeout(600);
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Pause', exact: true }).last()).toBeVisible();
    const answer = page.locator('[data-ui~="chat.message"][data-message-id]:visible')
      .filter({ has: page.locator('.message-assistant') }).last();
    await expect(answer).toBeVisible({ timeout: 30000 });
    await expect.poll(async () => (await answer.locator('.code-viewer .line-content')
      .evaluateAll(lines => lines.map(line => line.textContent).join('\n'))).trimEnd(), { timeout: 30000 }).toBe(expected);
    const id = await answer.getAttribute('data-message-id');
    await expect.poll(async () => (await dataApi('GET', `/messages/${id}`))?.status, { timeout: 10000 }).toBe('success');
    // Cherry 2.1.6 groups structured reasoning into its native process accordion.
    const toggle = answer.getByTestId('completed-process-trigger');
    await expect(toggle).toBeVisible();
    if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(answer.getByTestId('tool-history-content').getByText(thinking, { exact: true })).toBeVisible();
    await page.waitForTimeout(800);
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await page.waitForTimeout(600);
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await page.waitForTimeout(1500);
    const drawingBox = await answer.locator('.code-viewer').last().boundingBox();
    const promptBox = await page.getByText(prompt, { exact: true }).last().boundingBox();
    assert(drawingBox && drawingBox.y >= clip.y && drawingBox.y + drawingBox.height <= clip.y + clip.height,
      'the complete ASCII must fit in the recording');
    assert(promptBox && promptBox.y >= 100, 'the user prompt must remain below the fixed header');
    await page.screenshot({ path: path.join(evidence, 'streaming-final.png'), clip });
  } finally {
    recording = false;
    await capture;
  }
  if (frameFailure) throw frameFailure;
  assert(frames.length > 30 && frames.some(frame => frame.thinkingVisible));
  assert(frames.some(frame => frame.reasoningPanelVisible && frame.reasoningExpanded === true));
  assert(frames.some(frame => frame.reasoningPanelVisible && frame.reasoningExpanded === false),
    'the recording must demonstrate the real native reasoning collapse control');
  assert(frames.some(frame => frame.codeChars > 0 && frame.codeChars < expected.length),
    'recording must contain actual intermediate streaming states');
  assert.equal(frames.at(-1).codeChars, expected.length);
  assert(requests.some(record => record.path === cases[0].route && record.status === 200 && record.nativeReasoningSeen));
  assert(!requests.some(record => record.simulatedThinkingInBody), 'simulated reasoning must not appear in the normal content stream');
  const concat = ['ffconcat version 1.0'];
  for (let index = 0; index < frames.length; index++) {
    const duration = index + 1 < frames.length ? (frames[index + 1].timestampMs - frames[index].timestampMs) / 1000 : 0.125;
    concat.push(`file 'frames/${frames[index].file}'`, `duration ${Math.max(0.001, duration).toFixed(3)}`);
  }
  concat.push(`file 'frames/${frames.at(-1).file}'`);
  writeFileSync(path.join(evidence, 'frames.ffconcat'), concat.join('\n') + '\n');
  writeFileSync(path.join(evidence, 'recording.json'), JSON.stringify({
    client: 'Cherry Studio 2.1.6', viewport, clip, simulatedThinking: true, thinkingChannel: 'native', upstreamAI: false,
    capture: 'Native client frames with their original timing', frames,
    durationSeconds: (frames.at(-1).timestampMs + 125) / 1000
  }, null, 2));
  pass('recording: native reasoning_content, real foldable thinking panel, intermediate ASCII frames, and complete final drawing');
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

  if (recordStream) {
    await recordStreaming();
  } else if (showcase) {
    // These are native client screenshots of the stock testing template.
    // No responses, client markup, or artwork are rewritten for the capture.
    await page.evaluate(() => window.api.preference.setMultiple({ 'ui.theme_mode': 'dark' }));
    await page.reload();
    await expect(page.locator('[data-ui="app.shell"]').first()).toBeVisible({ timeout: 60000 });
    await select(cases[0].providerId, 'fakeapi-test');
    await send('你好', /^你好！(?:有什么可以帮你？|今天想聊些什么？)$/);
    await send('1+1', '2');
    await send('你是什么模型', '我是 FakeAPI 模拟服务，当前展示模型是 fakeapi-test。这条回复来自本地预设。');
    await page.waitForTimeout(2000); // Let the client's message fade-in finish.
    await page.screenshot({ path: path.join(evidence, '01-normal-tests.png') });
    pass('showcase: greeting, arithmetic, and transparent model identity from local presets');

    await page.setViewportSize({ width: 1280, height: 1750 });
    await select(cases[0].providerId, 'fakeapi-frog');
    const drawings = ['wave', 'stand', 'laugh', 'think'].map(pose =>
      readFileSync(path.join(process.env.FAKEAPI_TEST_ASSETS, `nailong/${pose}.txt`), 'utf8'));
    const fallback = await send('帮我写一个快速排序', drawings, true);
    await page.waitForTimeout(2000);
    const naturalBox = await fallback.locator('.code-viewer').last().boundingBox();
    assert(naturalBox);
    const fallbackHeight = Math.ceil(naturalBox.height + 600);
    await page.setViewportSize({ width: 1280, height: fallbackHeight });
    await page.waitForTimeout(1000); // Native scrolling responds to window resize.
    const fallbackBox = await fallback.locator('.code-viewer').last().boundingBox();
    assert(fallbackBox && fallbackBox.y >= 0 && fallbackBox.y + fallbackBox.height <= fallbackHeight,
      'the complete fallback ASCII must fit in the screenshot');
    const promptBox = await page.getByText('帮我写一个快速排序', { exact: true }).last().boundingBox();
    assert(promptBox && promptBox.y >= 100 && promptBox.y + promptBox.height <= fallbackHeight,
      'the triggering user message must be visible below the fixed client header');
    await page.screenshot({ path: path.join(evidence, '02-default-ascii.png') });
    pass('showcase: an ordinary task receives one complete, unchanged bundled ASCII pose');

    await page.setViewportSize({ width: 1280, height: 1250 });
    const wave = await send('/frog', drawings[0], true);
    await page.waitForTimeout(2000);
    const waveBox = await wave.locator('.code-viewer').last().boundingBox();
    assert(waveBox && waveBox.y >= 0 && waveBox.y + waveBox.height <= 1250,
      'the complete waving ASCII must fit in the screenshot');
    await page.screenshot({ path: path.join(evidence, '03-wave-easter-egg.png') });
    pass('showcase: the /frog easter egg renders the complete waving pose');

    assert(requests.some(record => record.path === cases[0].route && record.status === 200));
    await page.setViewportSize({ width: 1200, height: 1500 });
    await page.goto(new URL('docs/ascii-preview.html', `file://${root}/`).href);
    await expect(page).toHaveTitle('FakeAPI · 内置 ASCII');
    await expect(page.locator('main article')).toHaveCount(4);
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({ path: path.join(evidence, '04-four-built-in-poses.png'), fullPage: true });
    pass('showcase: existing repository preview, exactly four built-in poses');
  } else {
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
  const nativeStart = requests.length;
  const wave = readFileSync(path.join(process.env.FAKEAPI_TEST_ASSETS, 'nailong/wave.txt'), 'utf8');
  await send('/client/native', wave, true);
  await send('1+1', '2');
  assert(requests.slice(nativeStart).some(record => record.nativeReasoningSeen && record.status === 200),
    'native thinking stream must be observed before continuing the conversation');
  assert(requests.slice(nativeStart).some(record => record.fixture === 'math' && record.status === 200),
    'conversation must continue after native thinking');
  await page.screenshot({ path: path.join(evidence, 'native-thinking-multiturn.png') });
  pass('native reasoning_content response followed by a successful next turn in the real client');
  const laugh = readFileSync(path.join(process.env.FAKEAPI_TEST_ASSETS, 'nailong/laugh.txt'), 'utf8');
  const drawing = await send('/laugh', laugh, true);
  pass('ASCII material preserved exactly in the rendered code block');
  await drawing.locator('.code-viewer').last().screenshot({ path: path.join(evidence, 'ascii-laugh.png') });

  const cancelStart = requests.length;
  const composer = page.locator('[data-ui~="chat.composer"] [contenteditable="true"]').first();
  await composer.fill('');
  await composer.pressSequentially('/client/cancel', { delay: 20 });
  await expect(composer).toHaveText('/client/cancel');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const stop = page.getByRole('button', { name: 'Pause', exact: true }).last();
  await expect(stop).toBeVisible({ timeout: 20000 });
  await expect.poll(() => requests.slice(cancelStart).some(record => record.fixture === 'cancel'
    && record.status === 200 && record.responseBytes > 0), { timeout: 10000 }).toBe(true);
  const canceled = requests.slice(cancelStart).find(record => record.fixture === 'cancel');
  await stop.click();
  await expect.poll(() => canceled.clientAborted === true && canceled.upstreamClosed === true,
    { timeout: 10000 }).toBe(true);
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible({ timeout: 10000 });
  await select(cases[0].providerId, 'fakeapi-test');
  const recoveryStart = requests.length;
  await send('1+1', '2');
  assert(requests.slice(recoveryStart).some(record => record.fixture === 'math' && record.status === 200));
  await page.screenshot({ path: path.join(evidence, 'cancel-recovery.png') });
  pass('Stop generation releases the single request slot and allows a new reply');
  }
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
    browser: 'Browser plugin not available; Playwright CDP in isolated GitHub runner',
    mode: recordStream ? 'streaming' : showcase ? 'screenshots' : 'acceptance',
    viewport: page ? await page.viewportSize() : null,
    passed, success: !failure, failure, rendererErrors: errors, knownWarnings, requests,
    durationSeconds: Math.round((Date.now() - started) / 1000) }, null, 2));
  if (browser) await browser.close().catch(() => {});
  if (app && app.exitCode === null) app.kill('SIGTERM');
  await new Promise(resolve => proxy.close(resolve));
}
if (failure) process.exitCode = 1;
