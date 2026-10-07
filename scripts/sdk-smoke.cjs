// Deterministic browser/receipt qualification. No account, provider or network app.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {spawn, spawnSync} = require('node:child_process');

const root = path.resolve(__dirname, '..');
const work = fs.mkdtempSync(path.join(os.tmpdir(), '9lives-sdk-smoke-'));
const engine = path.join(work, '9l');
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

function sync(command, args, options = {}) {
  const result = spawnSync(command, args, {cwd: root, encoding: 'utf8', timeout: 60000, maxBuffer: 4 << 20, ...options});
  assert.ifError(result.error);
  return result;
}
function invoke(spec, options = []) {
  const result = sync(engine, ['run', spec, '--sdk', '--format', 'json', '--receipt-dir', path.join(work, 'receipts'), ...options]);
  return {code: result.status, summary: JSON.parse(result.stdout)};
}
function readEvents(receipt) {
  const raw = fs.readFileSync(receipt.evidence.stdoutPath, 'utf8');
  assert(!raw.includes('synthetic worker log'));
  assert(!raw.includes('Order confirmed'));
  assert(!raw.includes('injecting env')); // config stdout stays out of evidence
  return raw.trim().split('\n').map(line => JSON.parse(line));
}
function processTable() {
  // Process identities/status only: never collect arguments or environment.
  return sync('ps', ['-axo', 'pid=,ppid=,pgid=,stat=,comm=']).stdout.trim().split('\n').map(line => {
    const match = line.trim().match(/^(\d+)\s+(\d+)\s+(\d+)\s+(\S+)\s+(.+)$/);
    return match ? {pid: Number(match[1]), parent: Number(match[2]), group: Number(match[3]), state: match[4], name: match[5]} : null;
  }).filter(Boolean);
}
async function interrupted(kind) {
  const marker = path.join(work, `${kind}.json`);
  const receipts = path.join(work, kind);
  const child = spawn(engine, ['run', 'testdata/sdk/tests/slow.spec.ts', '--sdk', '--format', 'json', '--timeout', kind === 'timeout' ? '12s' : '30s', '--pass-env', 'NINELIVES_SMOKE_MARKER', '--receipt-dir', receipts], {
    cwd: root, env: {...process.env, NINELIVES_SMOKE_MARKER: marker}, stdio: ['ignore', 'pipe', 'pipe'],
  });
  let output = '';
  // Engine diagnostics are not needed for this synthetic smoke.
  child.stdout.on('data', chunk => { output += chunk; assert(output.length < 4 << 20); });
  child.stderr.resume();
  const completion = new Promise((resolve, reject) => { child.once('error', reject); child.once('close', code => resolve(code)); });
  const guard = setTimeout(() => child.kill('SIGTERM'), 40000);
  try {
    const start = Date.now();
    while (!fs.existsSync(marker)) {
      assert(Date.now() - start < 10000, 'real browser did not become ready');
      assert(child.exitCode === null, 'engine exited before real browser work');
      await sleep(50);
    }
    const {workerPID} = JSON.parse(fs.readFileSync(marker));
    const table = processTable();
    const worker = table.find(item => item.pid === workerPID);
    assert(worker, 'worker is running');
    // Chromium starts a separate group; include descendants as well as the
    // engine group so a leaked browser cannot hide behind a successful exit.
    const ownedIds = new Set(table.filter(item => item.group === worker.group).map(item => item.pid));
    for (let changed = true; changed;) {
      changed = false;
      for (const item of table) {
        if (ownedIds.has(item.parent) && !ownedIds.has(item.pid)) { ownedIds.add(item.pid); changed = true; }
      }
    }
    const owned = table.filter(item => ownedIds.has(item.pid));
    assert(owned.some(item => /chrom(e|ium)/i.test(item.name)), 'real Chromium is owned by the test worker');
    if (kind === 'cancel') {
      const states = fs.readdirSync(receipts).filter(name => name.startsWith('run-'));
      assert.equal(states.length, 1);
      assert.equal(sync(engine, ['cancel', states[0], '--receipt-dir', receipts]).status, 0);
    }
    assert.equal(await completion, 1);
    const summary = JSON.parse(output);
    const receipt = summary.receipts[0];
    assert.equal(receipt.status, kind === 'cancel' ? 'canceled' : 'timed_out');
    assert.equal(receipt.validated, false);
    assert(receipt.termination && receipt.termination.signal === 'SIGTERM', 'cleanup recorded');
    const deadline = Date.now() + 5000;
    while (true) {
      const live = processTable().filter(item => owned.some(original => original.pid === item.pid) && !item.state.startsWith('Z'));
      if (!live.length) break;
      assert(Date.now() < deadline, 'owned worker/browser processes survived cancellation');
      await sleep(100);
    }
    console.log(`SDK ${kind}: real worker/browser cleanup and terminal receipt verified`);
  } finally {
    clearTimeout(guard);
    if (child.exitCode === null) { child.kill('SIGTERM'); await completion; }
  }
}

async function main() {
  assert.equal(sync('go', ['build', '-o', engine, './cmd/9l']).status, 0);
  const passing = invoke('testdata/sdk/tests/checkout.spec.ts');
  assert.equal(passing.code, 0);
  assert.equal(passing.summary.complete, true);
  const receipt = passing.summary.receipts[0];
  assert.equal(receipt.status, 'passed');
  assert.equal(receipt.validated, true);
  assert.equal(receipt.executedTests, 1);
  assert.equal(receipt.verifiedAssertions, 0); // A step category is not proof of assertion coverage.
  const frames = readEvents(receipt);
  assert(frames.some(frame => frame.type === 'step_end' && frame.category === 'assertion'));
  assert(frames.some(frame => frame.type === 'test_end' && frame.artifacts.some(artifact => artifact.kind === 'json' && !artifact.retained)));
  for (const frame of frames) {
    assert.equal(frame.runId, receipt.runId);
    assert.equal(frame.jobId, receipt.jobId);
    assert.equal(frame.attemptId, receipt.attemptId);
  }
  const canonical = JSON.parse(fs.readFileSync(path.join(path.dirname(receipt.receiptPath), 'execution-receipt-1.0.json')));
  assert.equal(canonical.framework, 'playwright');
  assert.equal(canonical.verdict, 'passed');
  assert.equal(canonical.correlation.attempt_id, receipt.attemptId);
  assert.equal(canonical.counts.total, 1);
  assert.equal(canonical.counts.availability, 'available');
  const failing = invoke('testdata/sdk/tests/defect.spec.ts');
  assert.equal(failing.code, 1);
  assert.equal(failing.summary.receipts[0].status, 'failed');
  assert.equal(failing.summary.receipts[0].failureCount, 1);
  const skipped = invoke('testdata/sdk/tests/skipped.spec.ts');
  assert.equal(skipped.code, 1);
  assert.equal(skipped.summary.complete, false);
  assert.equal(skipped.summary.receipts[0].validated, false);
  assert.equal(skipped.summary.receipts[0].evidence.stdoutPath || '', '');
  // A declared pin accepts that skip; it stays counted as skipped, not executed.
  const pinned = invoke('testdata/sdk/tests/skipped.spec.ts', ['--pin-skip', 'skipped.spec.ts › selected test has no execution evidence']);
  assert.equal(pinned.code, 0);
  assert.equal(pinned.summary.complete, true);
  assert.equal(pinned.summary.receipts[0].validated, true);
  assert.equal(pinned.summary.receipts[0].executedTests, 1);
  assert.equal(pinned.summary.receipts[0].skippedTests, 1);
  const pinnedCanonical = JSON.parse(fs.readFileSync(path.join(path.dirname(pinned.summary.receipts[0].receiptPath), 'execution-receipt-1.0.json')));
  assert.equal(pinnedCanonical.counts.skipped, 1);
  assert.equal(pinnedCanonical.counts.passed, 1);
  const wrongPin = invoke('testdata/sdk/tests/skipped.spec.ts', ['--pin-skip', 'skipped.spec.ts › some other test']);
  assert.equal(wrongPin.code, 1);
  assert.equal(wrongPin.summary.complete, false);
  const nested = invoke('testdata/sdk/tests/nested/config.spec.ts');
  assert.equal(nested.code, 0);
  assert.equal(nested.summary.complete, true);
  assert.equal(nested.summary.receipts[0].executedTests, 1);
  const serial = invoke('testdata/sdk/tests/serial-retry.spec.ts');
  assert.equal(serial.code, 0);
  assert.equal(serial.summary.complete, true);
  assert.equal(serial.summary.receipts[0].validated, true);
  assert.equal(serial.summary.receipts[0].executedTests, 3);
  assert.equal(serial.summary.receipts[0].failureCount, 0);
  const serialFrames = readEvents(serial.summary.receipts[0]);
  assert.equal(serialFrames.filter(frame => frame.type === 'test_begin' && frame.retry === 1).length, 3);
  assert(serialFrames.some(frame => frame.type === 'test_end' && frame.retry === 0 && frame.status === 'skipped'));
  console.log('SDK checkout: browser assertions, step/artifact metadata and canonical receipts verified; business defect stays red');
  const missingProvider = invoke('testdata/sdk/tests/goal-start-failure.spec.ts');
  assert.equal(missingProvider.code, 1);
  assert.equal(missingProvider.summary.receipts[0].goalFailed, true);
  assert.equal(missingProvider.summary.receipts[0].failureCount, 0);
  const failedGoalCanonical = JSON.parse(fs.readFileSync(path.join(path.dirname(missingProvider.summary.receipts[0].receiptPath), 'execution-receipt-1.0.json')));
  assert.equal(failedGoalCanonical.verdict, 'failed');
  assert.equal(failedGoalCanonical.failure.code, 'goal.failed');
  assert.equal(failedGoalCanonical.counts.failed, 0);
  process.env.NINELIVES_SMOKE_INVALID_GOAL = '1';
  const invalidGoal = invoke('testdata/sdk/tests/goal-start-failure.spec.ts', ['--goal-script', 'testdata/sdk/click-script.json', '--pass-env', 'NINELIVES_SMOKE_INVALID_GOAL']);
  delete process.env.NINELIVES_SMOKE_INVALID_GOAL;
  assert.equal(invalidGoal.code, 1);
  assert.equal(invalidGoal.summary.receipts[0].goalFailed, true);
  assert.equal(invalidGoal.summary.receipts[0].failureCount, 0);
  const goal = invoke('testdata/sdk/tests/goal.spec.ts', ['--goal-script', 'testdata/sdk/goal-script.json']);
  assert.equal(goal.code, 0);
  assert.equal(goal.summary.receipts[0].goals[0].status, 'completed');
  const decisions = goal.summary.receipts[0].goals[0].decisions;
  assert(decisions.some(decision => decision.action === 'wait'));
  assert.deepEqual(decisions.filter(decision => ['fill', 'click'].includes(decision.action)).map(decision => decision.action), ['fill', 'click', 'click']);
  assert(!JSON.stringify(goal.summary).includes('Fixture Person'));
  process.env.NINELIVES_SMOKE_DEFECT = '1';
  const defect = invoke('testdata/sdk/tests/goal.spec.ts', ['--goal-script', 'testdata/sdk/goal-script.json', '--pass-env', 'NINELIVES_SMOKE_DEFECT']);
  delete process.env.NINELIVES_SMOKE_DEFECT;
  assert.equal(defect.code, 1);
  assert.equal(defect.summary.receipts[0].status, 'failed');
  assert.equal(defect.summary.receipts[0].goals[0].status, 'completed');
  const policy = invoke('testdata/sdk/tests/goal-policy.spec.ts', ['--goal-script', 'testdata/sdk/policy-script.json']);
  assert.equal(policy.code, 1);
  assert.equal(policy.summary.receipts[0].goals[0].status, 'policy_blocked');
  const redactedPolicy = invoke('testdata/sdk/tests/goal-redaction-policy.spec.ts', ['--goal-script', 'testdata/sdk/redaction-policy-script.json']);
  assert.equal(redactedPolicy.code, 1);
  assert.equal(redactedPolicy.summary.receipts[0].goals[0].status, 'policy_blocked');
  const cuePolicy = invoke('testdata/sdk/tests/goal-cue-policy.spec.ts', ['--goal-script', 'testdata/sdk/click-script.json']);
  assert.equal(cuePolicy.code, 1);
  assert.equal(cuePolicy.summary.receipts[0].goals[0].status, 'policy_blocked');
  const deadlineGoal = invoke('testdata/sdk/tests/goal-deadline.spec.ts', ['--goal-script', 'testdata/sdk/click-script.json']);
  assert.equal(deadlineGoal.code, 1);
  assert.equal(deadlineGoal.summary.receipts[0].goals[0].status, 'budget_exhausted');
  assert(readEvents(deadlineGoal.summary.receipts[0]).some(event => event.type === 'test_end' && event.status === 'passed'));
  const abortedGoal = invoke('testdata/sdk/tests/goal-abort.spec.ts', ['--goal-script', 'testdata/sdk/click-script.json']);
  assert.equal(abortedGoal.code, 1);
  assert.equal(abortedGoal.summary.receipts[0].goals[0].status, 'interrupted');
  assert(abortedGoal.summary.receipts[0].goals[0].decisions.some(decision => decision.action === 'click' && decision.outcome === 'pending'));
  assert(readEvents(abortedGoal.summary.receipts[0]).some(event => event.type === 'test_end' && event.status === 'passed'));
  const counter = path.join(work, 'goal-clicks.txt');
  process.env.NINELIVES_SMOKE_COUNTER = counter;
  const retriedGoal = invoke('testdata/sdk/tests/goal-retry.spec.ts', ['--goal-script', 'testdata/sdk/click-script.json', '--pass-env', 'NINELIVES_SMOKE_COUNTER']);
  delete process.env.NINELIVES_SMOKE_COUNTER;
  assert.equal(retriedGoal.code, 1);
  assert.equal(fs.readFileSync(counter, 'utf8'), 'click\n');
  assert.equal(retriedGoal.summary.receipts[0].goals.length, 1);
  // Page text reaching the provider: a value padded to straddle the old
  // 1000-character cut, and one split by whitespace, must both be redacted.
  const {chromium} = require('@playwright/test');
  const {observeState, valueRedactor} = require(path.join(root, 'packages/playwright/dist/goal.js'));
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage();
    await page.setContent(`<h1>${' \n'.repeat(490)}Welcome, Fixture Person</h1><p role="status">Order for Fixture\n   Person</p>`);
    const state = await observeState(page, valueRedactor({name: 'Fixture Person'}));
    assert.deepEqual(state, ['Welcome, [redacted]', 'Order for [redacted]']);
  } finally {
    await browser.close();
  }
  console.log('Goals: real Chromium multi-step execution, delayed controls, style drift, policy rejection and state redaction verified');
  await interrupted('timeout');
  await interrupted('cancel');
}
main().catch(error => { console.error(error.message); process.exitCode = 1; }).finally(() => fs.rmSync(work, {recursive: true, force: true}));
