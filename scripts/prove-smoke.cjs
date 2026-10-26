// Qualify `9l prove` against the synthetic shop fixture with real Chromium.
// The cart and order requests are asserted, recommendations are not: every
// fault on the first two must be caught and every fault on the third must
// survive, for the default kinds and again for the opt-in kinds, including the
// redirect to a reserved never-resolving origin and the held response. No
// account, provider or external server is involved.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');
const {spawnSync} = require('node:child_process');

const root = path.resolve(__dirname, '..');
const work = fs.mkdtempSync(path.join(os.tmpdir(), '9lives-prove-smoke-'));
const engine = path.join(work, process.platform === 'win32' ? '9l.exe' : '9l');
const receipts = path.join(work, 'receipts');
let stage = 'engine-build';

function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer().once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const {port} = server.address();
      server.close(() => resolve(port));
    });
  });
}

function invoke(command, args, extraEnv = {}) {
  const env = {};
  for (const key of ['PATH', 'HOME', 'TMPDIR', 'TEMP', 'TMP', 'SystemRoot', 'USERPROFILE', 'LOCALAPPDATA', 'APPDATA', 'PLAYWRIGHT_BROWSERS_PATH']) {
    if (process.env[key] !== undefined) env[key] = process.env[key];
  }
  const result = spawnSync(command, args, {cwd: root, env: {...env, ...extraEnv}, encoding: 'utf8', timeout: 300000, maxBuffer: 4 << 20});
  assert(!result.error, 'owned invocation failed or timed out');
  return result;
}

async function main() {
  assert.equal(invoke('go', ['build', '-o', engine, './cmd/9l']).status, 0, 'engine build failed');
  // Faults target the baseline's origin, so every run reuses one port.
  const port = String(await freePort());
  stage = 'prove';
  const proved = invoke(engine, ['prove', 'testdata/sdk/tests/prove-shop.spec.ts', '--format', 'json', '--paths', '--timeout', '60s',
    '--pass-env', 'NINELIVES_PROVE_SHOP_PORT', '--receipt-dir', receipts], {NINELIVES_PROVE_SHOP_PORT: port});
  stage = `prove exit ${proved.status}`;
  assert.equal(proved.status, 0);
  const report = JSON.parse(proved.stdout);
  stage = 'report';
  assert.equal(report.version, 1);
  assert.equal(report.policy, 'prove-network-v4');
  assert.deepEqual(report.faultKinds, ['abort', 'http-500', 'empty-json']);
  assert.equal(report.complete, true);
  assert.equal(report.baseline.status, 'passed');
  const byPath = Object.fromEntries(report.requests.map(request => [new URL(request.url).pathname, request.id]));
  assert.deepEqual(Object.keys(byPath).sort(), ['/api/cart', '/api/orders', '/api/recommendations']);
  const results = path => report.faults.filter(fault => fault.request === byPath[path]).map(fault => `${fault.kind}:${fault.result}`);
  stage = 'asserted requests';
  assert.deepEqual(results('/api/cart'), ['abort:caught', 'http-500:caught', 'empty-json:caught']);
  assert.deepEqual(results('/api/orders'), ['abort:caught', 'http-500:caught', 'empty-json:caught']);
  stage = 'unasserted request';
  assert.deepEqual(results('/api/recommendations'), ['abort:survived', 'http-500:survived', 'empty-json:survived']);
  assert.deepEqual(report.summary, {faults: 9, caught: 6, survived: 3, inconclusive: 0, notRun: 0, exercised: 9});
  assert(report.faults.every(fault => fault.applied > 0 && fault.runId));
  // One test in the spec: each fault carries exactly its result for that test.
  assert(report.faults.every(fault => fault.tests.length === 1 && fault.tests[0].result === fault.result && /^[a-f0-9]{64}$/.test(fault.tests[0].testId)));
  stage = 'receipts';
  for (const runId of [report.baseline.runId, ...report.faults.map(fault => fault.runId)]) {
    const result = invoke(engine, ['result', runId, '--receipt-dir', receipts]);
    assert.equal(result.status, 0);
    const [receipt] = JSON.parse(result.stdout).receipts;
    assert.equal(receipt.validated, true);
    assert.equal(receipt.executedTests, 1);
  }
  stage = 'persisted report';
  const saved = fs.readFileSync(path.join(receipts, 'proofs', `${report.baseline.runId}.json`), 'utf8');
  assert(!saved.includes('/api/') && !saved.includes('127.0.0.1'), 'persisted report must not contain request URLs');
  assert.deepEqual(JSON.parse(saved).summary, report.summary);

  // The opt-in kinds, which the SDK reports in the baseline's capabilities.
  stage = 'prove opt-in kinds';
  const optIn = ['http-401', 'http-403', 'http-429', 'malformed-json'];
  const extended = invoke(engine, ['prove', 'testdata/sdk/tests/prove-shop.spec.ts', '--faults', optIn.join(','), '--format', 'json', '--paths', '--timeout', '60s',
    '--pass-env', 'NINELIVES_PROVE_SHOP_PORT', '--receipt-dir', receipts], {NINELIVES_PROVE_SHOP_PORT: port});
  stage = `prove opt-in kinds exit ${extended.status}`;
  assert.equal(extended.status, 0);
  const second = JSON.parse(extended.stdout);
  stage = 'opt-in report';
  assert.equal(second.complete, true);
  assert.deepEqual(second.faultKinds, optIn);
  const secondByPath = Object.fromEntries(second.requests.map(request => [new URL(request.url).pathname, request.id]));
  const secondResults = path => second.faults.filter(fault => fault.request === secondByPath[path]).map(fault => `${fault.kind}:${fault.result}`);
  for (const path of ['/api/cart', '/api/orders']) assert.deepEqual(secondResults(path), optIn.map(kind => `${kind}:caught`), path);
  assert.deepEqual(secondResults('/api/recommendations'), optIn.map(kind => `${kind}:survived`));
  assert.deepEqual(second.summary, {faults: 12, caught: 8, survived: 4, inconclusive: 0, notRun: 0, exercised: 12});
  assert(second.faults.every(fault => fault.applied > 0 && fault.runId));

  // The redirect and delay kinds. The redirect is followed to redirect.invalid,
  // which never resolves, so the page's fetch rejects; the held response is
  // released only when the test's 2s expect timeout has already failed it or
  // the test has finished without waiting for it, so these runs stay short.
  stage = 'prove redirect and delay kinds';
  const timed = ['redirect-cross-origin', 'delay'];
  const startedThird = Date.now();
  const third = invoke(engine, ['prove', 'testdata/sdk/tests/prove-shop.spec.ts', '--faults', timed.join(','), '--format', 'json', '--paths', '--timeout', '60s',
    '--pass-env', 'NINELIVES_PROVE_SHOP_PORT', '--receipt-dir', receipts], {NINELIVES_PROVE_SHOP_PORT: port});
  stage = `prove redirect and delay kinds exit ${third.status}`;
  assert.equal(third.status, 0);
  const thirdReport = JSON.parse(third.stdout);
  stage = 'redirect and delay report';
  assert.equal(thirdReport.complete, true);
  assert.deepEqual(thirdReport.faultKinds, timed);
  const thirdByPath = Object.fromEntries(thirdReport.requests.map(request => [new URL(request.url).pathname, request.id]));
  const thirdResults = path => thirdReport.faults.filter(fault => fault.request === thirdByPath[path]).map(fault => `${fault.kind}:${fault.result}`);
  for (const path of ['/api/cart', '/api/orders']) assert.deepEqual(thirdResults(path), timed.map(kind => `${kind}:caught`), path);
  assert.deepEqual(thirdResults('/api/recommendations'), timed.map(kind => `${kind}:survived`));
  assert.deepEqual(thirdReport.summary, {faults: 6, caught: 4, survived: 2, inconclusive: 0, notRun: 0, exercised: 6});
  assert(thirdReport.faults.every(fault => fault.applied > 0 && fault.runId));
  // No delay run waited out the 35s hold: the context closing released it.
  assert(thirdReport.faults.filter(fault => fault.kind === 'delay').every(fault => fault.durationMs < 30000), 'a delay fault run outlasted the test');
  const thirdSeconds = Math.round((Date.now() - startedThird) / 1000);

  stage = 'delay keeps a server redirect';
  await delayKeepsRedirects();
  console.log(`Prove: real Chromium baselines; 9 default fault runs (6 caught on asserted requests, 3 survived on the unasserted one), 12 opt-in fault runs (8 caught, 4 survived) and 6 redirect/delay fault runs (4 caught, 2 survived, ${thirdSeconds}s in all); a held redirect is followed by Chromium's fetch and XHR; persisted report has no URLs`);
}

// delay holds a redirect as the server sent it, so Chromium follows it itself
// and the page sees the same redirect it sees without the fault, for fetch and
// XHR alike. The SDK's instrument is driven directly with a short hold. This
// page.evaluate runs outside invoke()'s subprocess timeout, so the page bounds
// each request, the smoke bounds the page, and cleanup runs on every failure.
const heldRequestLimitMs = 5000;
const heldEvaluateLimitMs = 10000;

function bounded(promise, ms, what) {
  let timer;
  const limit = new Promise((resolve, reject) => {
    timer = setTimeout(() => reject(new Error(`${what} did not settle within ${ms}ms`)), ms);
  });
  return Promise.race([promise, limit]).finally(() => clearTimeout(timer));
}

async function delayKeepsRedirects() {
  const http = require('node:http');
  const {chromium} = require('@playwright/test');
  const {instrument, ProveChannel} = require('../packages/playwright/dist/prove.js');
  const server = http.createServer((request, response) => {
    if (request.url === '/') return response.writeHead(200, {'content-type': 'text/html'}).end('<p>shop</p>');
    if (request.url === '/api/old') return response.writeHead(302, {location: '/api/new'}).end();
    if (request.url === '/api/new') return response.writeHead(200, {'content-type': 'application/json'}).end('{"ok":true}');
    response.writeHead(404).end();
  });
  let browser;
  try {
    await new Promise((resolve, reject) => server.once('error', reject).listen(0, '127.0.0.1', resolve));
    const origin = `http://127.0.0.1:${server.address().port}`;
    browser = await chromium.launch();
    for (const via of ['fetch', 'xhr']) {
      stage = `delay keeps a server redirect (${via})`;
      const file = path.join(work, `held-redirect-${via}.ndjson`);
      const channel = new ProveChannel(fs.openSync(file, 'wx', 0o600), {id: 'fault-1', kind: 'delay', method: 'GET', origin, path: '/api/old'});
      let context, seen, held;
      try {
        context = await browser.newContext();
        await instrument(context, channel, {holdMs: 300});
        const page = await context.newPage();
        await page.goto(origin + '/');
        const started = Date.now();
        seen = await bounded(page.evaluate(({via, limitMs}) => via === 'fetch'
          ? fetch('/api/old', {signal: AbortSignal.timeout(limitMs)}).then(async r => ({redirected: r.redirected, path: new URL(r.url).pathname, body: await r.text()}))
          : new Promise((resolve, reject) => {
            const xhr = new XMLHttpRequest();
            xhr.open('GET', '/api/old');
            xhr.timeout = limitMs;
            xhr.onload = () => resolve({redirected: new URL(xhr.responseURL).pathname !== '/api/old', path: new URL(xhr.responseURL).pathname, body: xhr.responseText});
            xhr.onerror = () => reject(new Error('xhr failed'));
            xhr.onabort = () => reject(new Error('xhr aborted'));
            xhr.ontimeout = () => reject(new Error(`xhr timed out after ${limitMs}ms`));
            xhr.send();
          }), {via, limitMs: heldRequestLimitMs}), heldEvaluateLimitMs, `${via} through a held redirect`);
        held = Date.now() - started;
      } finally {
        await context?.close();
        channel.close();
      }
      assert.deepEqual(seen, {redirected: true, path: '/api/new', body: '{"ok":true}'}, via);
      assert(held >= 250, `${via} response was not held`);
      assert.deepEqual(fs.readFileSync(file, 'utf8').trim().split('\n').map(line => JSON.parse(line)), [{type: 'applied', fault: 'fault-1'}], via);
    }
  } finally {
    try {
      await browser?.close();
    } finally {
      server.closeAllConnections();
      await new Promise(resolve => server.close(resolve));
    }
  }
}

main().catch(error => {
  const kind = error instanceof assert.AssertionError ? 'assertion' : error instanceof SyntaxError ? 'invalid-json' : 'execution';
  console.error(`Prove smoke failed: ${stage} (${kind})`);
  process.exitCode = 1;
}).finally(() => fs.rmSync(work, {recursive: true, force: true}));
