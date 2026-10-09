// Qualify the standalone Go artifact in consumer projects without npm installs.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const root = path.resolve(__dirname, '..');
const work = fs.mkdtempSync(path.join(os.tmpdir(), '9lives-assessment-portability-'));
const engine = path.join(work, process.platform === 'win32' ? '9l.exe' : '9l');
const env = {};
for (const key of ['PATH', 'HOME', 'TMPDIR', 'TEMP', 'TMP', 'SystemRoot', 'USERPROFILE', 'LOCALAPPDATA', 'APPDATA']) {
  if (process.env[key] !== undefined) env[key] = process.env[key];
}
let stage = 'Go-only build';
function invoke(command, args, cwd, extraEnv = {}) {
  return spawnSync(command, args, { cwd, env: { ...env, ...extraEnv }, encoding: 'utf8', timeout: 60000, maxBuffer: 2 << 20 });
}
try {
  const built = invoke('go', ['build', '-trimpath', '-o', engine, './cmd/9l'], root);
  assert(!built.error && built.status === 0);
  for (const consumer of ['no-typescript', 'incompatible-typescript']) {
    stage = consumer;
    const project = path.join(work, consumer);
    fs.mkdirSync(project);
    const source = "import {test,expect} from '@playwright/test'; import './missing'; throw Error('must not execute');\n" +
      "for (const name of ['one','two']) { test(`case ${name}`, () => { expect(1).toBe(1); }); }\n" +
      "test('helper', async () => { await verifyOrder(); });\n" +
      "test('empty', () => {});";
    const contract = { version: 1, requirements: [{ id: 'fixture', reference: 'fixture', revision: '1', expectedOutcomes: [{ id: 'fixture', description: 'Fixture' }] }] };
    fs.writeFileSync(path.join(project, 'fixture.spec.ts'), source);
    fs.writeFileSync(path.join(project, 'requirements.json'), JSON.stringify(contract));
    // A controlled substitute for the reported TS 7 package with no parser API.
    // Hooks and the package throw/write a marker if any consumer code is loaded.
    const poison = "require('node:fs').writeFileSync('executed', 'unexpected'); throw Error('consumer code loaded');";
    fs.writeFileSync(path.join(project, 'hook.cjs'), poison);
    if (consumer === 'incompatible-typescript') {
      const module = path.join(project, 'node_modules/typescript');
      fs.mkdirSync(module, { recursive: true });
      fs.writeFileSync(path.join(module, 'package.json'), '{"version":"7.0.2","main":"index.js"}');
      fs.writeFileSync(path.join(module, 'index.js'), poison);
    }
    const result = invoke(engine, ['assess', 'fixture.spec.ts', '--requirements', 'requirements.json', '--format', 'json'], project,
      { NODE_OPTIONS: '--require=' + path.join(project, 'hook.cjs'), NODE_PATH: path.join(project, 'node_modules') });
    assert(!result.error && result.status === 0 && result.stderr === '');
    const report = JSON.parse(result.stdout);
    assert.equal(report.version, 3);
    assert.equal(report.policy, 'assessment-source-v7');
    assert.equal(report.compiler, '5.9.3');
    assert.equal(report.execution, 'not_run');
    assert.equal(report.completeness, 'partial');
    assert.equal(report.tests.length, 3);
    assert.deepEqual(report.tests[0].findings.map(f => f.code), ['declaration-generation']);
    assert.deepEqual(report.tests[1].findings.map(f => f.code), ['unresolved-helper']);
    assert.equal(report.tests[1].dimensions.assertionAdequacy, 'unknown');
    assert.deepEqual(report.tests[2].findings.map(f => f.rule), ['no-direct-assertion']);
    assert(!fs.existsSync(path.join(project, 'executed')));
    fs.writeFileSync(path.join(project, 'fixture.spec.ts'), 'test(');
    const invalid = invoke(engine, ['assess', 'fixture.spec.ts', '--requirements', 'requirements.json', '--format', 'json'], project);
    assert(!invalid.error && invalid.status === 2 && invalid.stdout === '');
    assert.equal(invalid.stderr, '9l: assessment [syntax]: source is not valid syntax for the bundled TypeScript parser\n');
  }
  console.log('Go artifact assessed without consumer TypeScript; incompatible package and Node hooks ignored; syntax failure stayed private.');
} catch {
  console.error(`Assessment portability failed during ${stage}; raw subprocess diagnostics suppressed.`);
  process.exitCode = 1;
} finally {
  fs.rmSync(work, { recursive: true, force: true });
}
