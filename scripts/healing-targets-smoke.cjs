// Real-Chromium qualification of the first two-record slice of issue #80.
// This exercises ordinary 9l execution, not an automatic healing feature.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');

const root = path.resolve(__dirname, '..');
const work = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), '9lives-targets-')));
const engine = path.join(work, process.platform === 'win32' ? '9l.exe' : '9l');
const project = path.join(work, 'project');
const evidenceDir = path.join(root, '.context/healing-targets');
const hash = bytes => crypto.createHash('sha256').update(bytes).digest('hex');
const names = ['weak check accepts any open editor', 'strong check requires the intended record ID'];
let stage = 'build';

function invoke(command, args, extraEnv = {}) {
  const env = {};
  for (const key of ['PATH', 'HOME', 'TMPDIR', 'TEMP', 'TMP', 'SystemRoot', 'USERPROFILE', 'LOCALAPPDATA', 'APPDATA', 'PLAYWRIGHT_BROWSERS_PATH']) {
    if (process.env[key] !== undefined) env[key] = process.env[key];
  }
  const result = spawnSync(command, args, { cwd: root, env: { ...env, ...extraEnv }, encoding: 'utf8', timeout: 60000, maxBuffer: 4 << 20 });
  assert(!result.error, 'owned fixture invocation failed or timed out');
  return result;
}

function git(...args) {
  const result = invoke('git', ['-C', project, '-c', 'user.name=9lives fixture', '-c', 'user.email=fixture@example.invalid', '-c', 'commit.gpgsign=false', ...args]);
  assert.equal(result.status, 0, 'isolated fixture repository command failed');
}

function assertionLine(spec, expectedId) {
  const lines = fs.readFileSync(spec, 'utf8').split('\n');
  const matches = lines.flatMap((line, index) => line.includes(`.toHaveText('${expectedId}')`) ? [index + 1] : []);
  assert.equal(matches.length, 1, 'assertion attribution must be unambiguous');
  return matches[0];
}

function readResults(summary, expectedNames) {
  assert.equal(summary.receipts.length, 1);
  const receipt = summary.receipts[0];
  assert.equal(receipt.validated, true);
  assert.equal(receipt.executed, true);
  assert.equal(receipt.attempt, 1);
  assert.equal(receipt.executedTests, expectedNames.length);
  assert.equal(receipt.skippedTests, 0);
  assert.equal(receipt.agentProvenance.status, 'matched');
  assert(receipt.agentProvenance.after, 'provenance must be checked after execution');
  const raw = fs.readFileSync(receipt.evidence.stdoutPath);
  assert.equal(hash(raw), receipt.evidence.stdoutSha256);
  const report = JSON.parse(raw);
  assert.equal(report.errors.length, 0, 'global errors make the experiment inconclusive');
  const specs = [];
  const walk = suite => { specs.push(...(suite.specs || [])); (suite.suites || []).forEach(walk); };
  report.suites.forEach(walk);
  assert.deepEqual(specs.map(spec => spec.title), expectedNames);
  return { receipt, results: specs.map(spec => {
    assert.equal(spec.tests.length, 1);
    const test = spec.tests[0];
    assert.equal(test.expectedStatus, 'passed', 'expected failures cannot qualify the canary');
    assert.equal(test.results.length, 1, 'Playwright retries cannot qualify a control');
    assert.equal(test.results[0].retry, 0);
    return test.results[0];
  }) };
}

// Only the exact ID assertion in the known spec may detect this defect.
// Setup errors, hook errors, unrelated assertions and timeouts are inconclusive.
function classify(result, spec, line) {
  if (result.status === 'passed') return 'missed';
  if (result.status !== 'failed' || result.errors.length !== 1) return 'inconclusive';
  const error = result.errors[0];
  let sameFile = false;
  try { sameFile = fs.realpathSync(path.resolve(project, error.location?.file || '')) === fs.realpathSync(spec); } catch {}
  return sameFile && error.location?.line === line && /toHaveText/.test(error.message || '') ? 'detected' : 'inconclusive';
}

function readObservation(result, control) {
  const attachments = result.attachments.filter(a => a.name === 'target-observation');
  if (control === 'setup-failure' || control === 'timeout') {
    assert.equal(attachments.length, 0, 'setup must stop before any app action');
    return undefined;
  }
  assert.equal(attachments.length, 1);
  const attachment = attachments[0];
  assert.equal(attachment.contentType, 'application/json');
  assert.equal(typeof attachment.body, 'string');
  assert(attachment.body.length < 4096, 'fixture observation exceeds its bound');
  const observation = JSON.parse(Buffer.from(attachment.body, 'base64').toString('utf8'));
  const recordId = control === 'wrong-row' ? 'record-b' : 'record-a';
  // Exact shape excludes page content and proves a fresh, single action.
  assert.deepEqual(observation, {
    initialRecordIds: control === 'reordered' ? ['record-b', 'record-a'] : ['record-a', 'record-b'],
    events: [{ action: 'editor-opened', recordId }],
  });
  return observation;
}

try {
  fs.mkdirSync(evidenceDir, { recursive: true, mode: 0o700 });
  fs.rmSync(path.join(evidenceDir, 'qualification.json'), { force: true });
  assert.equal(invoke('go', ['build', '-o', engine, './cmd/9l']).status, 0, 'engine build failed');
  fs.cpSync(path.join(root, 'testdata/healing-targets'), project, {
    recursive: true, filter: source => !['node_modules', 'test-results'].includes(path.basename(source)),
  });
  // A throwaway repository supports provenance even on detached CI checkouts.
  git('init', '--quiet', '-b', 'fixture-qualification');
  git('add', 'fixture.ts', 'playwright.config.ts', 'package.json', 'requirements.json', 'tests');
  git('commit', '--quiet', '-m', 'Pin synthetic two-record fixture');
  fs.symlinkSync(path.join(root, 'node_modules'), path.join(project, 'node_modules'), process.platform === 'win32' ? 'junction' : 'dir');
  const spec = path.join(project, 'tests/records.spec.ts');
  const canary = path.join(project, 'tests/canary.spec.ts');
  const specHash = hash(fs.readFileSync(spec));
  const strongLine = assertionLine(spec, 'record-a');
  const canaryLine = assertionLine(canary, 'record-b');
  const provenanceFiles = new Map();
  for (const file of [spec, canary]) {
    stage = 'provenance';
    const captured = invoke(engine, ['provenance', file, '--agent', 'fixture-qualification']);
    assert.equal(captured.status, 0);
    const provenance = path.join(work, `${path.basename(file)}-provenance.json`);
    fs.writeFileSync(provenance, captured.stdout, { mode: 0o600 });
    provenanceFiles.set(file, provenance);
  }
  stage = 'source assessment';
  const assessed = invoke(engine, ['assess', spec, '--requirements', path.join(project, 'requirements.json'), '--agent-provenance', provenanceFiles.get(spec), '--format', 'json']);
  assert.equal(assessed.status, 0);
  const assessment = JSON.parse(assessed.stdout);
  assert.equal(assessment.sourceSHA256, specHash);
  assert.equal(assessment.agentProvenance.status, 'matched');
  assert.equal(assessment.tests.length, 2);
  assert.deepEqual(assessment.tests[0].findings.filter(f => f.rule === 'unmapped-outcome').map(f => f.outcome), ['exact-record-id']);
  assert.equal(assessment.tests[1].findings.filter(f => f.rule === 'unmapped-outcome').length, 0);
  assert(assessment.tests.every(t => t.dimensions.runtimeEvidence === 'unknown'));

  const observations = {};
  for (const control of ['baseline', 'wrong-row', 'reordered', 'canary', 'setup-failure', 'timeout']) {
    stage = `${control} execution`;
    const file = control === 'canary' ? canary : spec;
    const execution = invoke(engine, ['run', file, '--format', 'json', '--agent-provenance', provenanceFiles.get(file),
      '--timeout', '20s', '--deadline', '30s', '--attempts', '1', '--pass-env', 'NINELIVES_TARGET_CONTROL', '--receipt-dir', path.join(work, 'receipts')],
    { NINELIVES_TARGET_CONTROL: control === 'canary' ? 'baseline' : control });
    const passing = control === 'baseline' || control === 'reordered';
    assert.equal(execution.status, passing ? 0 : 1);
    const summary = JSON.parse(execution.stdout);
    assert.equal(summary.complete, passing);
    const { receipt, results } = readResults(summary, control === 'canary' ? ['wrong expected ID canary'] : names);
    stage = `${control} attribution`;
    const classifications = results.map(result => classify(result, file, control === 'canary' ? canaryLine : strongLine));
    if (passing) assert(results.every(result => result.status === 'passed'));
    if (control === 'wrong-row') assert.deepEqual(classifications, ['missed', 'detected']);
    if (control === 'canary') assert.deepEqual(classifications, ['detected']);
    if (control === 'setup-failure' || control === 'timeout') assert(classifications.every(value => value === 'inconclusive'));
    const appObservations = results.map(result => readObservation(result, control));
    assert.equal(hash(fs.readFileSync(spec)), specHash, 'main assertions must remain unchanged');
    observations[control] = {
      executionStatuses: results.map(result => result.status),
      controlOutcomes: passing ? results.map(() => 'not_applicable') : classifications,
      appObservations,
      runId: receipt.runId, attemptId: receipt.attemptId, status: receipt.status,
      validated: receipt.validated, executedTests: receipt.executedTests,
      provenance: receipt.agentProvenance,
      evidenceSHA256: receipt.evidence.stdoutSha256,
    };
  }
  stage = 'persist bounded qualification';
  const qualification = {
    version: 1, issue: 'https://github.com/Quality-Max/9lives-runner/issues/80',
    sourceSHA256: specHash, canarySHA256: hash(fs.readFileSync(canary)),
    fixtureSHA256: hash(fs.readFileSync(path.join(project, 'fixture.ts'))),
    configSHA256: hash(fs.readFileSync(path.join(project, 'playwright.config.ts'))),
    requirementsSHA256: assessment.requirementsSHA256,
    node: process.version, playwright: require('@playwright/test/package.json').version,
    assertionLines: { strong: strongLine, canary: canaryLine }, observations,
    limits: [
      'Synthetic real-Chromium fixture only; no general healing or semantic correctness guarantee.',
      'Wrong-row selection is an explicit fixture control, not a 9l prove mutation capability.',
      'Runtime provenance snapshots are declared fixture-qualification records, not authenticated agent authorship.',
      'No live provider, external app, competing-candidate healing or automatic ambiguity guard was exercised.',
    ],
  };
  fs.writeFileSync(path.join(evidenceDir, 'qualification.json'), JSON.stringify(qualification, null, 2) + '\n', { mode: 0o600 });
  fs.writeFileSync(path.join(evidenceDir, 'report.json'), JSON.stringify(assessment, null, 2) + '\n', { mode: 0o600 });
  console.log('Target fixture qualified in real Chromium: wrong row passes weak check, fails exact ID; reordered rows pass; wrong-ID canary fails; setup failure and timeout stay inconclusive. Six validated 9l receipts.');
} catch {
  console.error(`Target qualification failed during ${stage}; no success claim. Inspect locally without publishing raw test logs.`);
  process.exitCode = 1;
} finally {
  try { fs.unlinkSync(path.join(project, 'node_modules')); } catch {}
  fs.rmSync(work, { recursive: true, force: true });
}
