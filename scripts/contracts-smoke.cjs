// Validate actual CLI/receipt documents independently of the Go schema generator.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {spawnSync} = require('node:child_process');

const root = path.resolve(__dirname, '..');
const work = fs.mkdtempSync(path.join(os.tmpdir(), '9lives-contract-smoke-'));
const engine = path.join(work, process.platform === 'win32' ? '9l.exe' : '9l');
const receipts = path.join(work, 'receipts');
const samples = [];
let active = 'build';

function command(binary, args, options = {}) {
  const result = spawnSync(binary, args, {cwd: root, encoding: 'utf8', timeout: 60000, maxBuffer: 4 << 20, ...options});
  assert.ifError(result.error);
  return result;
}

function emit(schema, args, code = 0) {
  active = schema;
  const result = command(engine, [...args, '--format', 'json']);
  assert.equal(result.status, code);
  const document = JSON.parse(result.stdout);
  samples.push({schema, document, valid: true});
  return document;
}

try {
  assert.equal(command('go', ['build', '-o', engine, './cmd/9l']).status, 0);
  emit('version', ['version']);
  emit('plan', ['plan', 'testdata/sdk/tests/checkout.spec.ts', '--sdk']);
  const passed = emit('run-result', ['run', 'testdata/sdk/tests/checkout.spec.ts', '--sdk', '--receipt-dir', receipts]);
  emit('run-result', ['result', passed.runId, '--receipt-dir', receipts]);
  emit('run-status', ['status', passed.runId, '--receipt-dir', receipts]);
  const failed = emit('run-result', ['run', 'testdata/sdk/tests/goal-mixed-failure.spec.ts', '--sdk', '--receipt-dir', receipts], 1);
  emit('run-result', ['run', 'testdata/sdk/tests/goal-start-failure.spec.ts', '--sdk', '--receipt-dir', receipts], 3);
  // A scripted goal records real decisions. A script sends no provider
  // request, so its decisions carry no request count or model.
  const goal = emit('run-result', ['run', 'testdata/sdk/tests/goal.spec.ts', '--sdk', '--goal-script', 'testdata/sdk/goal-script.json', '--receipt-dir', receipts]);
  const decision = document => document.receipts[0].goals[0].decisions[0];
  assert.ok(decision(goal) && !('requests' in decision(goal)) && !('model' in decision(goal)));
  emit('assess', ['assess', 'testdata/sdk/tests/checkout.spec.ts']);
  emit('assess-suite', ['assess', 'testdata/sdk/tests/checkout.spec.ts', 'testdata/sdk/tests/defect.spec.ts']);
  // A receipt that did not pass always carries its metadata-only failure summary.
  assert.equal(passed.receipts[0].failureSummary, undefined);
  assert.equal(typeof failed.receipts[0].failureSummary?.stop, 'string');
  for (const receipt of [...passed.receipts, ...failed.receipts, ...goal.receipts]) {
    samples.push({schema: 'receipt', document: JSON.parse(fs.readFileSync(receipt.receiptPath, 'utf8')), valid: true});
  }
  const events = fs.readFileSync(path.join(receipts, passed.runId, 'events.jsonl'), 'utf8').trim().split('\n');
  for (const event of events) samples.push({schema: 'progress-event', document: JSON.parse(event), valid: true});
  for (const mutate of [
    doc => { doc.version = 99; },
    doc => { doc.outcome = 'unknown'; },
    doc => { delete doc.outcome; },
    doc => { doc.receipts[0].status = 'unknown'; },
    doc => { doc.receipts[0].nonGoalFailureCount = 'one'; },
    doc => { delete doc.receipts[0].failureSummary.stop; },
    doc => { doc.receipts[0].failureSummary.failedTests = 'one'; },
  ]) {
    const document = structuredClone(failed);
    mutate(document);
    samples.push({schema: 'run-result', document, valid: false});
  }
  // A provider's reported request count and model are optional, typed fields.
  for (const [mutate, valid] of [
    [entry => { entry.requests = 2; entry.model = 'fixture-model'; }, true],
    [entry => { entry.requests = 0; }, true],
    [entry => { entry.requests = 'two'; }, false],
    [entry => { entry.model = 7; }, false],
  ]) {
    const document = structuredClone(goal);
    mutate(decision(document));
    samples.push({schema: 'run-result', document, valid});
  }
  active = 'independent-validator';
  const validation = command(process.env.NINELIVES_CONTRACT_PYTHON || 'python3', ['scripts/validate-cli-contracts.py'], {input: JSON.stringify(samples)});
  assert.equal(validation.status, 0);
  console.log(`CLI contracts: ${samples.filter(item => item.valid).length} documents across 8 schemas validated; ${samples.filter(item => !item.valid).length} invalid mutations rejected`);
} catch (error) {
  // Never echo process output, document bodies or exception payloads.
  console.error(`CLI contract smoke failed at ${active} (${error instanceof assert.AssertionError ? 'assertion' : 'execution'})`);
  process.exitCode = 1;
} finally {
  fs.rmSync(work, {recursive: true, force: true});
}
