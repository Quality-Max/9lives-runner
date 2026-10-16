// Reuse the release lab's synthetic shop and unchanged specs. No model calls.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const net = require('node:net');
const {createHash} = require('node:crypto');
const {execFile} = require('node:child_process');

const root = path.resolve(__dirname, '../..');
const source = path.join(root, 'demo/release-lab');
fs.mkdirSync(path.join(root, '.context'), {recursive: true});
const work = fs.mkdtempSync(path.join(root, '.context/first-run-'));
const project = path.join(work, 'project');
const receipts = path.join(work, 'receipts');
const engine = path.join(work, process.platform === 'win32' ? '9l.exe' : '9l');
const summary = {version: 1, complete: false, revision: '', platform: `${process.platform}/${process.arch}`, sources: {}, results: []};
const save = (name, data) => fs.writeFileSync(path.join(work, name), JSON.stringify(data, null, 2) + '\n');
const hash = file => createHash('sha256').update(fs.readFileSync(file)).digest('hex');
const env = {};
for (const key of ['PATH', 'HOME', 'TMPDIR', 'TEMP', 'TMP', 'SystemRoot', 'USERPROFILE', 'LOCALAPPDATA', 'APPDATA', 'PLAYWRIGHT_BROWSERS_PATH']) {
  if (process.env[key] !== undefined) env[key] = process.env[key];
}
const cancellation = new AbortController();
for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => cancellation.abort());
let stage = 'setup';

function invoke(command, args, cwd = project) {
  return new Promise((resolve, reject) => {
    const child = execFile(command, args, {cwd, env, signal: cancellation.signal,
      encoding: 'utf8', timeout: 180000, maxBuffer: 4 << 20, killSignal: 'SIGTERM'}, (error, stdout) => {
      if (error) return reject(new Error('Command failed, timed out, or was interrupted'));
      resolve(stdout);
    });
    child.stdin.end();
  });
}
const cli = async args => JSON.parse(await invoke(engine, args));

async function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer().once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const {port} = server.address();
      server.close(error => error ? reject(error) : resolve(String(port)));
    });
  });
}

async function checkReceipt(runId, status) {
  const result = await cli(['result', runId, '--receipt-dir', receipts]);
  assert.equal(result.receipts.length, 1);
  const [receipt] = result.receipts;
  assert.equal(receipt.validated, true);
  assert.equal(receipt.executedTests, 1);
  assert.equal(receipt.status, status);
}

async function main() {
  assert.equal(process.argv.length, 2, 'This example takes no arguments');
  fs.mkdirSync(project);
  for (const item of ['app', 'tests', 'playwright.config.ts', 'package.json', 'requirements.json']) {
    fs.cpSync(path.join(source, item), path.join(project, item), {recursive: true});
  }
  for (const file of ['tests/ordinary.spec.ts', 'tests/checkout.spec.ts', 'tests/checkout-complete.spec.ts', 'tests/fixtures.ts', 'app/server.cjs', 'app/index.html', 'app/order.cjs', 'playwright.config.ts', 'requirements.json']) {
    summary.sources[file] = hash(path.join(project, file));
  }
  summary.revision = (await invoke('git', ['rev-parse', 'HEAD'], root)).trim();
  save('summary.json', summary);
  stage = 'build CLI (requires Go 1.25.13 or newer)';
  await invoke('go', ['build', '-o', engine, './cmd/9l'], root);

  stage = 'ordinary Playwright run';
  const ordinary = await cli(['run', 'tests/ordinary.spec.ts', '--timeout', '30s', '--attempts', '1', '--format', 'json', '--receipt-dir', receipts]);
  assert.equal(ordinary.outcome, 'passed');
  assert.equal(ordinary.complete, true);
  assert.equal(ordinary.receipts.length, 1);
  await checkReceipt(ordinary.runId, 'passed');
  summary.results.push({name: 'ordinary Playwright', runId: ordinary.runId, status: 'passed'});
  save('summary.json', summary);
  console.log('1/3 Ordinary Playwright: one executed test, validated passing receipt.');

  // A stable origin lets the experiment replay baseline requests in isolation.
  env.LAB_PORT = await freePort();
  for (const [name, spec, caught, survived] of [
    ['missing recommendation check', 'checkout', 2, 1],
    ['complete synthetic contract', 'checkout-complete', 3, 0],
  ]) {
    stage = `HTTP-500 experiment: ${name}`;
    const report = await cli(['prove', `tests/${spec}.spec.ts`, '--faults', 'http-500', '--paths', '--format', 'json',
      '--timeout', '30s', '--pass-env', 'LAB_PORT', '--receipt-dir', receipts]);
    assert.equal(report.complete, true);
    assert.equal(report.baseline.status, 'passed');
    assert.equal(report.baseline.executedTests, 1);
    assert.deepEqual(report.faultKinds, ['http-500']);
    assert.deepEqual(report.summary, {faults: 3, caught, survived, inconclusive: 0, notRun: 0, exercised: 3});
    const requests = Object.fromEntries(report.requests.map(request => [request.id, new URL(request.url).pathname]));
    assert.deepEqual(Object.values(requests).sort(), ['/api/cart', '/api/orders', '/api/recommendations']);
    await checkReceipt(report.baseline.runId, 'passed');
    for (const fault of report.faults) {
      const expected = spec === 'checkout' && requests[fault.request] === '/api/recommendations' ? 'survived' : 'caught';
      assert.equal(fault.result, expected);
      assert(fault.applied > 0);
      assert.equal(fault.tests.length, 1);
      assert.equal(fault.tests[0].result, expected);
      assert.equal(fault.assertionFailedTests, expected === 'caught' ? 1 : 0);
      await checkReceipt(fault.runId, expected === 'caught' ? 'failed' : 'passed');
    }
    // Paths are from this known synthetic localhost fixture only; no page bodies.
    save(`${spec}-proof.json`, report);
    summary.results.push({name, baselineRunId: report.baseline.runId, ...report.summary});
    save('summary.json', summary);
    console.log(`${summary.results.length}/3 ${name}: healthy baseline passed; ${caught} faults caught, ${survived} survived.`);
  }
  for (const [file, digest] of Object.entries(summary.sources)) assert.equal(hash(path.join(project, file)), digest, 'Fixture changed during execution');
  summary.complete = true;
  save('summary.json', summary);
  console.log(`Evidence: ${work}\nChecked only this synthetic Chromium fixture; no production coverage claim.`);
}

main().catch(() => {
  summary.complete = false;
  summary.failedStage = stage;
  save('summary.json', summary);
  console.error(`First-run example incomplete at: ${stage}. Check Node, Go, npm ci, and Chromium installation.\nEvidence: ${work}`);
  process.exitCode = 1;
});
