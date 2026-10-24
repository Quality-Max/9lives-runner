const assert = require('node:assert/strict');
const test = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { diagnose, specChecks } = require('../dist/doctor.js');

test('an isolated empty workspace gives installation steps without writes or config execution', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), '9l-doctor-empty-'));
  try {
    const report = diagnose({ cliVersion: '0.4.7', sdk: true }, root);
    assert.equal(report.ready, false);
    assert.equal(report.checks.find(c => c.name === 'workspace').status, 'missing');
    assert.equal(report.checks.find(c => c.name === 'playwright-test').fix, 'npm install --save-dev @playwright/test@1.64.0');
    assert.equal(report.checks.find(c => c.name === 'sdk').fix, 'npm install --save-dev @9l/playwright@0.4.7');
    assert.deepEqual(fs.readdirSync(root), []);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('package metadata is bounded and redacted, and project config is never loaded', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), '9l-doctor-package-'));
  const pkg = path.join(root, 'node_modules', '@playwright/test');
  try {
    fs.mkdirSync(pkg, { recursive: true });
    fs.writeFileSync(path.join(root, 'package.json'), '{}');
    fs.writeFileSync(path.join(root, 'playwright.config.js'), 'throw new Error("fixture-config-must-not-run")');
    fs.writeFileSync(path.join(pkg, 'index.js'), 'throw new Error("fixture-package-entry-must-not-run")');
    fs.writeFileSync(path.join(pkg, 'package.json'), JSON.stringify({ name: '@playwright/test', version: '1.64.0', privateData: 'fixture-metadata-canary' }));
    const report = diagnose({ cliVersion: '0.4.7' }, root);
    assert.equal(report.checks.find(c => c.name === 'playwright-test').status, 'ok');
    assert.equal(report.ready, false, 'unknown browser registry is never ready');
    assert.equal(report.checks.find(c => c.name === 'run/browser').fix, 'npx --yes playwright@1.64.0 install chromium');
    assert.doesNotMatch(JSON.stringify(report), /fixture-|node_modules|9l-doctor-package/);
    fs.writeFileSync(path.join(pkg, 'package.json'), JSON.stringify({ name: '@playwright/test', version: 'fixture-invalid-metadata-canary' }));
    assert.doesNotMatch(JSON.stringify(diagnose({}, root)), /fixture-/);
    fs.writeFileSync(path.join(pkg, 'package.json'), JSON.stringify({ name: '@playwright/test', version: '1.64.0', oversized: 'x'.repeat(256 * 1024) }));
    assert.equal(diagnose({}, root).checks.find(c => c.name === 'playwright-test').status, 'missing');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

const engine = { protocol: '9l.engine/1', capabilities: ['steps', 'artifact-metadata', 'terminal-outcomes'], faults: ['abort', 'http-500', 'empty-json', 'http-401'] };

// A project whose installed @9l/playwright has the given manifest fields. The
// package entry throws, so any check that loaded it would fail.
function sdkProject(manifest) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), '9l-doctor-sdk-'));
  const pkg = path.join(root, 'node_modules', '@9l', 'playwright');
  fs.mkdirSync(pkg, { recursive: true });
  fs.writeFileSync(path.join(root, 'package.json'), '{}');
  fs.writeFileSync(path.join(pkg, 'index.js'), 'throw new Error("fixture-sdk-entry-must-not-run")');
  fs.writeFileSync(path.join(pkg, 'package.json'), JSON.stringify({ name: '@9l/playwright', main: 'index.js', ...manifest }));
  return root;
}

function sdkChecks(manifest, args) {
  const root = sdkProject(manifest);
  try {
    const report = diagnose({ cliVersion: '0.4.9', engine, ...args }, root);
    assert.doesNotMatch(JSON.stringify(report), /fixture-|node_modules|9l-doctor-sdk/);
    return { sdk: report.checks.find(c => c.name === 'sdk'), faults: report.checks.find(c => c.name === 'sdk/faults') };
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
}

test('a Node release above the minimum is supported and never told to downgrade', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), '9l-doctor-node-'));
  try {
    const node = diagnose({ cliVersion: '0.4.9' }, root).checks.find(c => c.name === 'node');
    const major = Number(process.versions.node.split('.')[0]);
    assert.ok(major >= 22, 'the test suite itself needs Node 22 or newer');
    assert.equal(node.fix, undefined, 'a supported Node has no install step');
    assert.equal(node.status, major === 24 ? 'ok' : 'warning');
    if (major !== 24) assert.match(node.message, /Supported but outside the qualified matrix/);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('the SDK pair is judged by its declared protocol and capabilities, not by version string', () => {
  // The same release: qualified.
  assert.equal(sdkChecks({ version: '0.4.9', '9l': engine }).sdk.status, 'ok');
  // Unless it declares a contract this CLI rejects: a linked or rebuilt SDK keeps the version.
  const rebuilt = { version: '0.4.9', '9l': { ...engine, protocol: '9l.engine/2' } };
  assert.equal(sdkChecks(rebuilt, { sdk: true }).sdk.status, 'missing');
  assert.match(sdkChecks(rebuilt, { sdk: true }).sdk.message, /Incompatible/);
  assert.equal(sdkChecks(rebuilt).sdk.status, 'warning');
  assert.equal(sdkChecks({ version: '0.4.9', '9l': { ...engine, capabilities: ['steps'] } }, { sdk: true }).sdk.status, 'missing');
  // An older release that declares the protocol this CLI admits runs, but is not the qualified pair.
  const admitted = sdkChecks({ version: '0.4.6', '9l': { ...engine, faults: ['abort', 'http-500', 'empty-json'] } }, { sdk: true });
  assert.equal(admitted.sdk.status, 'warning');
  assert.match(admitted.sdk.message, /Admitted: protocol 9l\.engine\/1 and capabilities match/);
  assert.equal(admitted.sdk.fix, 'npm install --save-dev @9l/playwright@0.4.9');
  assert.equal(admitted.faults.status, 'warning');
  assert.match(admitted.faults.message, /Not applied by this SDK: http-401\./);
  // A different protocol is rejected by run --sdk, so it is a missing prerequisite when the SDK is required.
  const foreign = { version: '0.4.6', '9l': { ...engine, protocol: '9l.engine/2' } };
  assert.equal(sdkChecks(foreign, { sdk: true }).sdk.status, 'missing');
  assert.match(sdkChecks(foreign, { sdk: true }).sdk.message, /Incompatible: the SDK declares protocol 9l\.engine\/2/);
  assert.equal(sdkChecks(foreign).sdk.status, 'warning');
  assert.equal(sdkChecks({ version: '0.4.6', '9l': { ...engine, capabilities: ['steps'] } }, { sdk: true }).sdk.status, 'missing');
  // A release that predates the declaration, or declares it badly, is only known at run time.
  for (const manifest of [{ version: '0.4.6' }, { version: '0.4.6', '9l': { protocol: 'fixture-protocol-canary\n', capabilities: [], faults: [] } }]) {
    const undeclared = sdkChecks(manifest, { sdk: true });
    assert.equal(undeclared.sdk.status, 'warning');
    assert.match(undeclared.sdk.message, /does not declare its engine protocol/);
    assert.match(undeclared.faults.message, /does not declare its prove fault kinds/);
  }
  // A CLI that sends no engine contract falls back the same way.
  assert.match(sdkChecks({ version: '0.4.6', '9l': engine }, { engine: undefined }).sdk.message, /does not declare its engine protocol/);
});

// A project directory holding the given files. Every fixture starts with a
// throw, so any check that executed a spec or config would fail.
const poison = 'throw new Error("fixture-spec-must-not-run");\n';
const sdkSpec = poison + "import { test, expect } from '@9l/playwright';\ntest('x', async ({ page }) => { await page.goto('/'); });\n";
function specProject(files) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), '9l-doctor-spec-'));
  for (const [name, content] of Object.entries(files)) {
    fs.mkdirSync(path.dirname(path.join(root, name)), { recursive: true });
    fs.writeFileSync(path.join(root, name), content);
  }
  return root;
}
const byName = (checks, name) => checks.find(c => c.name === name);

test('spec/import and spec/fixtures are read from the spec text without executing it', () => {
  const root = specProject({
    'sdk.spec.ts': sdkSpec,
    'required.spec.js': poison + 'const { test } = require("@9l/playwright");\ntest("x", async ({ context }) => { await context.newPage(); });\n',
    'plain.spec.ts': poison + 'import { expect } from "@9l/playwright";\nimport { test } from "@playwright/test";\ntest("x", async ({ page }) => {});\n',
    'plain.spec.cjs': poison + "const { test } = require('@playwright/test');\ntest('x', async ({ page }) => {});\n",
    'local.spec.ts': poison + "import { test } from './fixtures';\ntest('x', async ({ page }) => {});\n",
    'api.spec.ts': poison + "import { test, expect } from '@9l/playwright';\ntest('x', async ({ request }) => { await request.get('/'); });\n",
  });
  try {
    const sdk = specChecks(path.join(root, 'sdk.spec.ts'));
    assert.equal(byName(sdk, 'spec/import').status, 'ok');
    assert.equal(byName(sdk, 'spec/fixtures').status, 'ok');
    const required = specChecks(path.join(root, 'required.spec.js'));
    assert.equal(byName(required, 'spec/import').status, 'ok');
    assert.equal(byName(required, 'spec/fixtures').status, 'ok');
    for (const name of ['plain.spec.ts', 'plain.spec.cjs']) {
      const plain = byName(specChecks(path.join(root, name)), 'spec/import');
      assert.equal(plain.status, 'missing', name);
      assert.equal(plain.fix, 'import test from @9l/playwright; prove instruments only its page and context fixtures');
    }
    const local = byName(specChecks(path.join(root, 'local.spec.ts')), 'spec/import');
    assert.equal(local.status, 'warning');
    assert.match(local.message, /not followed/);
    const api = specChecks(path.join(root, 'api.spec.ts'));
    assert.equal(byName(api, 'spec/import').status, 'ok');
    assert.equal(byName(api, 'spec/fixtures').status, 'warning');
    assert.match(byName(api, 'spec/fixtures').message, /this spec uses neither/);
    for (const name of fs.readdirSync(root)) {
      const report = specChecks(path.join(root, name));
      assert.equal(byName(report, 'spec/config').status, 'warning', name);
      assert.match(byName(report, 'spec/config').message, /no playwright\.config\.\* above the spec/);
      assert.doesNotMatch(JSON.stringify(report), /fixture-|9l-doctor-spec/, name);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('spec/config names the nearest config and verifies only a literal testDir', () => {
  const root = specProject({
    'playwright.config.ts': poison + "export default { testDir: './tests' };\n",
    'tests/inside.spec.ts': sdkSpec,
    'other/outside.spec.ts': sdkSpec,
    'nested/playwright.config.mjs': poison + "const dir = 'specs';\nexport default { testDir: dir };\n",
    'nested/specs/computed.spec.ts': sdkSpec,
  });
  try {
    const inside = byName(specChecks(path.join(root, 'tests', 'inside.spec.ts')), 'spec/config');
    assert.equal(inside.status, 'ok');
    assert.equal(inside.message, 'config found: playwright.config.ts. The spec is under its literal testDir.');
    const outside = byName(specChecks(path.join(root, 'other', 'outside.spec.ts')), 'spec/config');
    assert.equal(outside.status, 'warning');
    assert.equal(outside.message, 'config found: playwright.config.ts. The spec is outside its literal testDir, so prove will not select it.');
    assert.match(outside.fix, /prove --config/);
    const computed = byName(specChecks(path.join(root, 'nested', 'specs', 'computed.spec.ts')), 'spec/config');
    assert.equal(computed.status, 'ok');
    assert.equal(computed.message, 'config found: playwright.config.mjs. testDir not verified: only a literal testDir can be read without executing the config.');
    // The adapter tries .ts before .js, so a .ts beside a .js is the one named.
    fs.writeFileSync(path.join(root, 'playwright.config.js'), poison);
    assert.match(byName(specChecks(path.join(root, 'tests', 'inside.spec.ts')), 'spec/config').message, /^config found: playwright\.config\.ts\./);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('spec messages carry paths only with --paths, and the spec flows through diagnose', () => {
  const root = specProject({ 'playwright.config.ts': poison + 'export default { testDir: "./tests" };\n', 'tests/a.spec.ts': sdkSpec });
  try {
    const spec = path.join(root, 'tests', 'a.spec.ts');
    const quiet = diagnose({ cliVersion: '0.5.0', sdk: true, spec }, root);
    assert.doesNotMatch(JSON.stringify(quiet), /fixture-|9l-doctor-spec|\.\/tests/);
    assert.equal(byName(quiet.checks, 'spec/import').status, 'ok');
    assert.equal(byName(quiet.checks, 'spec/config').status, 'ok');
    assert.ok(quiet.unverified.includes('testDir and testMatch unless literal'));
    assert.ok(!diagnose({ cliVersion: '0.5.0', sdk: true }, root).unverified.includes('testDir and testMatch unless literal'));
    const loud = diagnose({ cliVersion: '0.5.0', sdk: true, spec, paths: true }, root);
    const config = byName(loud.checks, 'spec/config');
    assert.ok(config.message.includes(`config found: playwright.config.ts in ${root}.`), config.message);
    assert.match(config.message, /under its literal testDir \(\.\/tests\)\.$/);
    // Only an absolute spec path is inspected; the CLI always sends one.
    assert.equal(byName(diagnose({ cliVersion: '0.5.0', spec: path.join('tests', 'a.spec.ts') }, root).checks, 'spec/import'), undefined);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('an absent, directory or oversized spec is reported without reading it', () => {
  const root = specProject({ 'big.spec.ts': 'x'.repeat(1024 * 1024 + 1), 'playwright.config.ts': poison });
  try {
    const big = specChecks(path.join(root, 'big.spec.ts'));
    assert.equal(byName(big, 'spec').status, 'warning');
    assert.match(byName(big, 'spec').message, /spec too large to inspect/);
    assert.equal(byName(big, 'spec/import'), undefined);
    assert.equal(byName(big, 'spec/config').status, 'ok', 'the config is still named');
    for (const spec of [path.join(root, 'absent.spec.ts'), root]) {
      const report = specChecks(spec);
      assert.equal(byName(report, 'spec').status, 'missing');
      assert.equal(byName(report, 'spec/import'), undefined);
    }
    assert.doesNotMatch(JSON.stringify(specChecks(path.join(root, 'absent.spec.ts'))), /9l-doctor-spec/);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('the executable check reports only what the CLI compared', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), '9l-doctor-exe-'));
  try {
    const check = executable => byName(diagnose({ cliVersion: '0.5.0', executable }, root).checks, 'executable');
    assert.deepEqual(check({ samePath: false, otherOnPath: false }), { name: 'executable', status: 'ok', message: 'no other 9l on PATH.' });
    assert.equal(check({ samePath: true, otherOnPath: true }).status, 'ok');
    const differs = check({ samePath: false, otherOnPath: true });
    assert.equal(differs.status, 'warning');
    assert.match(differs.message, /^the 9l first on PATH is not this executable; terminals, scripts and MCP hosts start that one unless configured with a full path\.$/);
    assert.match(differs.fix, /full path$/);
    const named = check({ samePath: false, otherOnPath: true, self: '/fixture/self/9l', other: '/fixture/other/9l' });
    assert.match(named.message, /This executable: \/fixture\/self\/9l\. First on PATH: \/fixture\/other\/9l\.$/);
    assert.equal(byName(diagnose({ cliVersion: '0.5.0' }, root).checks, 'executable'), undefined);
    assert.equal(diagnose({ cliVersion: '0.5.0', executable: { samePath: true, otherOnPath: true } }, root).checks[0].name, 'executable', 'a stale PATH is the first line a user reads');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});
