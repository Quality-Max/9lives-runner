// Qualify one defect hypothesis in an isolated synthetic checkout, using the
// ordinary Go execution/receipt core. Never mutate a user's app or test suite.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');

const root = path.resolve(__dirname, '..');
const work = fs.mkdtempSync(path.join(os.tmpdir(), '9lives-assessment-'));
const engine = path.join(work, process.platform === 'win32' ? '9l.exe' : '9l');
const project = path.join(work, 'project');
const hash = bytes => crypto.createHash('sha256').update(bytes).digest('hex');
let stage = 'build';
function invoke(command, args, extraEnv = {}) {
  const env = {};
  // Explicit non-secret keys only; the fixture control is forwarded separately.
  for (const key of ['PATH', 'HOME', 'TMPDIR', 'TEMP', 'TMP', 'SystemRoot', 'USERPROFILE', 'LOCALAPPDATA', 'APPDATA', 'PLAYWRIGHT_BROWSERS_PATH']) {
    if (process.env[key] !== undefined) env[key] = process.env[key];
  }
  const result = spawnSync(command, args, { cwd: root, env: { ...env, ...extraEnv }, encoding: 'utf8', timeout: 60000, maxBuffer: 4 << 20 });
  assert(!result.error, 'owned fixture invocation failed or timed out');
  return result;
}
function readResults(summary) {
  stage += ' receipt count';
  assert.equal(summary.receipts.length, 1);
  const receipt = summary.receipts[0];
  stage += ` validated=${receipt.validated}`;
  assert.equal(receipt.validated, true);
  assert.equal(receipt.executedTests, 3);
  stage += ' checksum';
  const raw = fs.readFileSync(receipt.evidence.stdoutPath);
  assert.equal(hash(raw), receipt.evidence.stdoutSha256);
  const report = JSON.parse(raw);
  stage += ' report';
  assert.equal(report.errors.length, 0, 'global errors make control inconclusive');
  const specs = [];
  const walk = suite => { specs.push(...(suite.specs || [])); (suite.suites || []).forEach(walk); };
  report.suites.forEach(walk);
  assert.equal(specs.length, 3);
  return specs.map(spec => {
    assert.equal(spec.tests.length, 1);
    assert.equal(spec.tests[0].results.length, 1, 'retries cannot qualify a control');
    return spec.tests[0].results[0];
  });
}
// This classifier qualifies only the known count assertion in this fixture.
// Failed setup, timeout and unrelated assertions are always inconclusive.
function classify(result, spec, line) {
  if (result.status === 'passed') return 'missed';
  if (result.status !== 'failed' || result.errors.length !== 1) return 'inconclusive';
  const error = result.errors[0];
  let sameFile = false;
  try { sameFile = fs.realpathSync(path.resolve(project, error.location?.file || '')) === fs.realpathSync(spec); } catch {}
  return sameFile && error.location?.line === line && /toHaveLength/.test(error.message || '')
    ? 'detected' : 'inconclusive';
}

try {
  assert.equal(invoke('go', ['build', '-o', engine, './cmd/9l']).status, 0, 'engine build failed');
  fs.cpSync(path.join(root, 'testdata/assessment'), project, { recursive: true });
  fs.symlinkSync(path.join(root, 'node_modules'), path.join(project, 'node_modules'), process.platform === 'win32' ? 'junction' : 'dir');
  const spec = path.join(project, 'tests/checkout.spec.ts');
  const source = fs.readFileSync(spec, 'utf8');
  const countLine = source.split('\n').findIndex(line => line.includes('expect(orders).toHaveLength(1)')) + 1;
  const requirements = path.join(project, 'requirements.json');
  const assessed = invoke(engine, ['assess', spec, '--requirements', requirements, '--format', 'json']);
  stage = 'source assessment';
  assert.equal(assessed.status, 0, 'assessment failed');
  const assessment = JSON.parse(assessed.stdout);
  assert.equal(assessment.tests.length, 3);
  assert.deepEqual(assessment.tests[0].findings.filter(f => f.rule === 'unmapped-outcome').map(f => f.outcome), ['order-count', 'order-items']);
  assert.equal(assessment.tests[1].findings.length, 0);
  assert.equal(assessment.tests[2].findings.length, 0);
  assert(assessment.tests.every(t => t.dimensions.runtimeEvidence === 'unknown'));
  const observations = {};
  for (const control of ['baseline', 'missing-order', 'setup-failure', 'timeout']) {
    stage = control;
    const execution = invoke(engine, ['run', spec, '--format', 'json', '--timeout', '20s', '--deadline', '30s', '--pass-env', 'NINELIVES_ASSESS_CONTROL', '--receipt-dir', path.join(work, 'receipts')], { NINELIVES_ASSESS_CONTROL: control });
    stage = `${control} execution exit ${execution.status}`;
    assert.equal(execution.status, control === 'baseline' ? 0 : 1, 'unexpected execution outcome');
    const summary = JSON.parse(execution.stdout);
    stage = `${control} completeness=${summary.complete}`;
    assert.equal(summary.complete, control === 'baseline', 'unexpected run completeness');
    const results = readResults(summary);
    stage = `${control} assertion attribution`;
    if (control === 'baseline') assert(results.every(r => r.status === 'passed'), 'baseline must pass');
    const outcomes = results.map(r => classify(r, spec, countLine));
    if (control === 'missing-order') assert.deepEqual(outcomes, ['missed', 'missed', 'detected']);
    if (control === 'setup-failure' || control === 'timeout') assert(outcomes.every(o => o === 'inconclusive'));
    observations[control] = { executionStatuses: results.map(r => r.status), controlOutcomes: control === 'baseline' ? ['not_applicable', 'not_applicable', 'not_applicable'] : outcomes,
      runId: summary.receipts[0].runId, attemptId: summary.receipts[0].attemptId, evidenceSHA256: summary.receipts[0].evidence.stdoutSha256 };
  }
  const qualification = { version: 1, policy: assessment.policy, sourceSHA256: assessment.sourceSHA256, requirementsSHA256: assessment.requirementsSHA256,
    node: process.version, playwright: require('@playwright/test/package.json').version,
    hypothesis: 'Confirmation succeeds while the local order service creates no order.', countAssertionLine: countLine,
    observations, limits: ['Synthetic real-Chromium qualification of one fixture and one defect; no general semantic correctness claim.', 'The selected-item assertion passes in baseline but has no separate negative control.'] };
  // Keep only bounded summaries, never raw browser reports or error payloads.
  const evidenceDir = path.join(root, '.context/assessment');
  fs.mkdirSync(evidenceDir, { recursive: true, mode: 0o700 });
  fs.writeFileSync(path.join(evidenceDir, 'qualification.json'), JSON.stringify(qualification, null, 2) + '\n', { mode: 0o600 });
  fs.writeFileSync(path.join(evidenceDir, 'report.json'), JSON.stringify(assessment, null, 2) + '\n', { mode: 0o600 });
  console.log('Checkout qualified: banner misses missing order; count assertion detects it; setup failure and timeout remain inconclusive.');
} catch {
  console.error(`Assessment qualification failed during ${stage}; no success claim. Inspect the fixture locally without publishing raw test logs.`);
  process.exitCode = 1;
} finally {
  fs.rmSync(work, { recursive: true, force: true });
}
