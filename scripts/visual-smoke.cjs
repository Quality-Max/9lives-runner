// Qualify `9l run --headed` with real Chromium for both adapters. The page
// itself reports whether the browser is headless, so each run proves the mode
// it ran in; a headless run that expects a headed browser must fail.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {spawnSync} = require('node:child_process');

const root = path.resolve(__dirname, '..');
const work = fs.mkdtempSync(path.join(os.tmpdir(), '9lives-visual-smoke-'));
const engine = path.join(work, '9l');
const receipts = path.join(work, 'receipts');
const fixtures = [
  {adapter: 'playwright-sdk', spec: 'testdata/sdk/tests/visual.spec.ts', args: ['--sdk']},
  {adapter: 'playwright', spec: 'testdata/playwright/tests/visual.spec.ts', args: []},
];
let stage = 'engine-build';

const onPath = name => (process.env.PATH || '').split(path.delimiter).some(dir => {
  try { fs.accessSync(path.join(dir, name), fs.constants.X_OK); return true; } catch { return false; }
});
const hasDisplay = process.platform === 'darwin' || !!(process.env.DISPLAY || process.env.WAYLAND_DISPLAY);

function invoke(args, {display = false, headed = false} = {}) {
  const env = {};
  const keys = ['PATH', 'HOME', 'TMPDIR', 'PLAYWRIGHT_BROWSERS_PATH'];
  if (display) keys.push('DISPLAY', 'WAYLAND_DISPLAY', 'XAUTHORITY');
  for (const key of keys) {
    if (process.env[key] !== undefined) env[key] = process.env[key];
  }
  if (headed) env.NINELIVES_SMOKE_HEADED = '1';
  // Without a display of our own, a virtual one is the only way to show a browser.
  const [command, ...prefix] = display && !hasDisplay ? ['xvfb-run', '-a', engine] : [engine];
  const result = spawnSync(command, [...prefix, ...args], {cwd: root, env, encoding: 'utf8', timeout: 180000, maxBuffer: 4 << 20});
  assert(!result.error, 'owned invocation failed or timed out');
  return result;
}

function run(fixture, {headed, expectHeaded, display}) {
  const args = ['run', fixture.spec, ...fixture.args, '--format', 'json', '--timeout', '60s', '--receipt-dir', receipts, '--pass-env', 'NINELIVES_SMOKE_HEADED'];
  if (headed) args.push('--headed');
  const result = invoke(args, {display, headed: expectHeaded});
  // xvfb-run merges the program's stderr, which carries the run-started line,
  // into stdout; the summary is the last line either way.
  const [receipt] = JSON.parse(result.stdout.trim().split('\n').pop()).receipts;
  assert.equal(receipt.adapter, fixture.adapter);
  assert.equal(receipt.evidence.command.includes('--headed'), headed);
  return {code: result.status, receipt};
}

function main() {
  assert.equal(spawnSync('go', ['build', '-o', engine, './cmd/9l'], {cwd: root}).status, 0, 'engine build failed');
  assert(hasDisplay || onPath('xvfb-run'), 'visual smoke needs a display or xvfb-run');
  for (const fixture of fixtures) {
    stage = `${fixture.adapter} headless`;
    let outcome = run(fixture, {headed: false, expectHeaded: false});
    assert.equal(outcome.code, 0);
    assert.equal(outcome.receipt.status, 'passed');
    assert.equal(outcome.receipt.validated, true);
    stage = `${fixture.adapter} headed`;
    outcome = run(fixture, {headed: true, expectHeaded: true, display: true});
    assert.equal(outcome.code, 0);
    assert.equal(outcome.receipt.status, 'passed');
    assert.equal(outcome.receipt.validated, true);
    stage = `${fixture.adapter} control`;
    outcome = run(fixture, {headed: false, expectHeaded: true});
    assert.equal(outcome.code, 1, 'a headless browser must fail the headed expectation');
    assert.equal(outcome.receipt.status, 'failed');
  }
  stage = 'no display';
  if (process.platform !== 'darwin') {
    const refused = invoke(['run', fixtures[0].spec, '--sdk', '--headed', '--receipt-dir', receipts]);
    assert.equal(refused.status, 2);
    assert.match(refused.stderr, /--headed needs a display/);
  }
  stage = 'workers';
  const plan = args => JSON.parse(invoke(['plan', fixtures[0].spec, '--sdk', '--format', 'json', ...args]).stdout).limits.maxParallel;
  assert.equal(plan(['--headed']), 1, 'headed runs default to one job at a time');
  assert.equal(plan(['--headed', '--workers', '3']), 3, 'an explicit --workers is kept');
  console.log('Visual: --headed verified from the page in both adapters, headless control fails, no-display refusal and one-job default verified');
}

try {
  main();
} catch (error) {
  const kind = error instanceof assert.AssertionError ? 'assertion' : error instanceof SyntaxError ? 'invalid-json' : 'execution';
  console.error(`Visual smoke failed: ${stage} (${kind})`);
  process.exitCode = 1;
} finally {
  fs.rmSync(work, {recursive: true, force: true});
}
