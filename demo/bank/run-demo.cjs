// Execute the public, signed-out bank demo and publish only checked evidence.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');

const root = path.resolve(__dirname, '../..');
const demo = path.join(root, 'demo');
fs.mkdirSync(path.join(root, '.context'), { recursive: true });
const work = fs.mkdtempSync(path.join(root, '.context/demo-bank-'));
const engine = path.join(work, process.platform === 'win32' ? '9l.exe' : '9l');
const spec = 'tests/bank.spec.ts';
const hash = text => crypto.createHash('sha256').update(text).digest('hex');
let stage = 'build';

// Do not inherit provider credentials or print raw browser/CLI diagnostics.
const childEnv = {};
for (const key of ['PATH', 'HOME', 'TMPDIR', 'TEMP', 'TMP', 'SystemRoot',
  'USERPROFILE', 'LOCALAPPDATA', 'APPDATA', 'PLAYWRIGHT_BROWSERS_PATH']) {
  if (process.env[key] !== undefined) childEnv[key] = process.env[key];
}
function invoke(command, args, cwd = work, expected = 0) {
  const result = spawnSync(command, args, {
    cwd, env: childEnv, encoding: 'utf8', timeout: 120000,
    maxBuffer: 4 << 20, stdio: ['ignore', 'pipe', 'pipe'],
  });
  if (result.error || result.status !== expected) {
    stage += ` (exit ${result.status ?? 'none'}, expected ${expected})`;
    throw new Error('command did not finish as expected');
  }
  return result.stdout;
}
function run(args, expected = 0) {
  return JSON.parse(invoke(engine, args, work, expected));
}
function announce(message) { console.log(message); }

try {
  announce('Building the local 9l runner…');
  invoke('go', ['build', '-o', engine, './cmd/9l'], root);
  fs.cpSync(path.join(__dirname, 'tests'), path.join(work, 'tests'), { recursive: true });
  fs.copyFileSync(path.join(__dirname, 'playwright.config.ts'), path.join(work, 'playwright.config.ts'));
  fs.copyFileSync(path.join(__dirname, 'package.json'), path.join(work, 'package.json'));
  fs.copyFileSync(path.join(__dirname, 'requirements.json'), path.join(work, 'requirements.json'));
  fs.mkdirSync(path.join(work, '.context'));
  const draft = fs.readFileSync(path.join(work, spec), 'utf8');

  stage = 'creation provenance';
  const provenance = run(['provenance', spec, '--agent', 'codex']);
  fs.writeFileSync(path.join(work, 'provenance.json'), JSON.stringify(provenance));
  announce('1/5  Loaded the checked-in agent draft; recorded its source hash.');

  stage = 'assessment';
  const assess = run(['assess', spec, '--requirements', 'requirements.json',
    '--agent-provenance', 'provenance.json', '--format', 'json']);
  const findingCodes = [...new Set(assess.tests.flatMap(test => test.findings.map(f => f.rule)))];
  assert(findingCodes.includes('fixed-wait'));
  assert.equal(assess.sourceSHA256, hash(draft));
  announce('2/5  Assessment found the seeded fixed wait (advisory).');

  // This is an explicit demo edit, not a claim that assess rewrites tests.
  const source = draft.replace('  // Seeded fixed wait for advisory assessment; removed before execution.\n  await page.waitForTimeout(400);\n', '');
  assert.notEqual(source, draft);
  fs.writeFileSync(path.join(work, spec), source);

  stage = 'original execution';
  const original = run(['run', spec, '--format', 'json'], 1);
  stage += ` (outcome ${original.outcome}, failed ${original.failed}, passed ${original.passed})`;
  assert.equal(original.outcome, 'failed');
  assert.equal(original.failed, 1);
  assert.equal(original.passed, 0);
  announce('3/5  Original test failed. Healing will recheck the failure.');

  stage = 'offline healing';
  const healed = run(['heal', spec, '--provider', 'none']);
  assert.equal(healed.state, 'verified');
  assert.equal(healed.original.passed, false);
  assert.equal(healed.original.executedTests, 1);
  assert.match(healed.original.failure, /getByRole\('button', \{ name: 'Log In', exact: true \}\)/);
  assert.equal(healed.verified.passed, true);
  assert.equal(healed.verified.executedTests, 1);
  assert.equal(healed.providerCalls, 0);
  assert.equal(healed.applied, false);
  const candidate = fs.readFileSync(path.join(work, `${spec}.healed`), 'utf8');
  assert.equal(candidate, source.replace('name: "Log In"', 'name: "Sign In"'));
  assert.equal(fs.readFileSync(path.join(work, spec), 'utf8'), source);
  assert.equal(healed.originalHash, hash(source));
  assert.equal(healed.candidateHash, hash(candidate));
  announce('4/5  Offline repair: Log In → Sign In. Exactly one locator literal changed.');
  announce('5/5  Candidate passed 1 test in isolation. Assertions unchanged; original not applied.');

  stage = 'evidence export';
  const revision = invoke('git', ['rev-parse', 'HEAD'], root).trim();
  const evidence = {
    date: new Date().toISOString().slice(0, 10), runnerRevision: revision,
    target: 'https://qualitymax-demo-bank.vercel.app/', platform: `${process.platform}/${process.arch}`,
    scope: 'One signed-out form test in real Chromium. No credentials entered or banking transactions. Public credential hint removed before captures.',
    generation: { agent: 'codex', mode: 'Checked-in agent-authored draft; replay does not call a model.', sourceSHA256: hash(draft) },
    assessment: { advisory: true, completeness: assess.completeness, findingCodes, fixedWaitRemovedBeforeExecution: true },
    original: { outcome: original.outcome, passed: original.passed, failed: original.failed, sourceSHA256: hash(source) },
    healing: { state: healed.state, provider: 'none', providerCalls: healed.providerCalls,
      executedTests: healed.verified.executedTests, candidateVerified: true, applied: healed.applied,
      onlyLocatorEdit: true, assertionsPreservedByteForByte: true, candidateSHA256: hash(candidate) },
    presentation: 'Edited replay from real browser captures and checked CLI results. Seeded stale locator, not an observed application regression or speed benchmark.',
  };
  // Export only the checked summary and captures after every assertion succeeds.
  for (const name of ['demo-bank-login.png', 'demo-bank-validation.png']) {
    fs.copyFileSync(path.join(work, '.context', name), path.join(demo, name));
  }
  fs.writeFileSync(path.join(demo, 'demo-bank-evidence.json'), JSON.stringify(evidence, null, 2) + '\n');
  announce('Evidence and browser captures saved to demo/. Render the GIF with demo/render-workflow.py.');
} catch {
  console.error(`Demo stopped during ${stage}. No passing result claimed. Check the live site and local prerequisites, then retry.`);
  process.exitCode = 1;
} finally {
  fs.rmSync(work, { recursive: true, force: true });
}
