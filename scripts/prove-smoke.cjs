// Qualify `9l prove` against the synthetic shop fixture with real Chromium.
// The cart and order requests are asserted, recommendations are not: every
// fault on the first two must be caught and every fault on the third must
// survive. No account, provider or external server is involved.
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
  assert.equal(report.policy, 'prove-network-v2');
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
  console.log('Prove: real Chromium baseline, 9 isolated fault runs, 6 caught on asserted requests, 3 survived on the unasserted request; persisted report has no URLs');
}

main().catch(error => {
  const kind = error instanceof assert.AssertionError ? 'assertion' : error instanceof SyntaxError ? 'invalid-json' : 'execution';
  console.error(`Prove smoke failed: ${stage} (${kind})`);
  process.exitCode = 1;
}).finally(() => fs.rmSync(work, {recursive: true, force: true}));
