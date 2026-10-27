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
    assert.match(byName(api, 'spec/fixtures').message, /no destructured page or context parameter found in a test or hook callback/);
    for (const name of fs.readdirSync(root)) {
      const report = specChecks(path.join(root, name));
      assert.equal(byName(report, 'spec/config').status, 'warning', name);
      assert.match(byName(report, 'spec/config').message, /no playwright\.config\.\* above the spec/);
      assert.doesNotMatch(JSON.stringify(report), /fixture-|9l-doctor-spec/, name);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// Comments, string contents and object arguments are not code the spec runs
// or fixtures it takes, so they do not change a verdict.
test('spec checks ignore comments, strings and object arguments', () => {
  const root = specProject({
    'commented.spec.ts': poison + "// import { test } from '@9l/playwright';\n/* import { test } from \"@9l/playwright\" */\nimport { test } from '@playwright/test';\ntest('x', async ({ page }) => {});\n",
    'object.spec.ts': poison + "import { test, expect } from '@9l/playwright';\ntest('x', async ({ request }) => { expect(await request.get('/')).toMatchObject({ page: 1, context: {} }); });\n",
    'url.spec.ts': poison + "import { test } from '@9l/playwright';\nconst base = 'http://localhost:3000'; // comment after a URL\ntest('x', async ({ page }) => { await page.goto(base); });\n",
    'fn.spec.ts': poison + "const { test } = require('@9l/playwright');\ntest('x', async function ({ context }) { await context.newPage(); });\n",
    'computed/playwright.config.ts': poison + "export default { testDir: './tests' + '/e2e' };\n",
    'computed/tests/a.spec.ts': sdkSpec,
    'commented-dir/playwright.config.ts': poison + "export default {\n  // testDir: './tests',\n  testDir: './e2e',\n};\n",
    'commented-dir/tests/a.spec.ts': sdkSpec,
    'projects/playwright.config.ts': poison + "export default { projects: [{ name: 'a', testDir: './tests' }, { name: 'b', testDir: './other' }] };\n",
    'projects/tests/a.spec.ts': sdkSpec,
  });
  try {
    const commented = specChecks(path.join(root, 'commented.spec.ts'));
    assert.equal(byName(commented, 'spec/import').status, 'missing', 'a commented-out SDK import does not hide the real one');
    const object = specChecks(path.join(root, 'object.spec.ts'));
    assert.equal(byName(object, 'spec/fixtures').status, 'warning', 'an object argument is not a fixture');
    assert.equal(byName(specChecks(path.join(root, 'url.spec.ts')), 'spec/fixtures').status, 'ok', 'a // inside a string is not a comment');
    assert.equal(byName(specChecks(path.join(root, 'fn.spec.ts')), 'spec/fixtures').status, 'ok', 'function-form callbacks count');
    const computed = byName(specChecks(path.join(root, 'computed', 'tests', 'a.spec.ts')), 'spec/config');
    assert.equal(computed.status, 'ok');
    assert.match(computed.message, /testDir not verified: only a literal testDir/);
    const commentedDir = byName(specChecks(path.join(root, 'commented-dir', 'tests', 'a.spec.ts')), 'spec/config');
    assert.equal(commentedDir.status, 'warning', 'the commented testDir is ignored and the live one applies');
    assert.match(commentedDir.message, /outside its literal testDir/);
    const projects = byName(specChecks(path.join(root, 'projects', 'tests', 'a.spec.ts')), 'spec/config');
    assert.equal(projects.status, 'ok');
    assert.match(projects.message, /testDir not verified: the config defines projects/);
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
    const absent = check({ samePath: false, otherOnPath: false });
    assert.equal(absent.status, 'warning', 'hosts configured with plain 9l cannot start it');
    assert.match(absent.message, /^no 9l on PATH; terminals, scripts and MCP hosts configured with plain 9l cannot start it\.$/);
    assert.match(absent.fix, /full path$/);
    for (const samePath of [true, false]) {
      const relative = check({ samePath, otherOnPath: true, relative: true });
      assert.equal(relative.status, 'warning', `relative entry, samePath ${samePath}`);
      assert.match(relative.message, /relative PATH entry that resolves against the current directory/);
      assert.match(relative.fix, /absolute directory/);
    }
    assert.equal(check({ samePath: false, otherOnPath: true, selfUnknown: true }).status, 'warning');
    assert.equal(check({ samePath: true, otherOnPath: true }).status, 'ok');
    assert.equal(check({ samePath: true, otherOnPath: true }).message, 'this executable is the 9l first on PATH.');
    // --paths names both files whatever the comparison found.
    const same = check({ samePath: true, otherOnPath: true, self: '/fixture/bin/9l', other: '/fixture/bin/9l' });
    assert.equal(same.status, 'ok');
    assert.equal(same.message, 'this executable is the 9l first on PATH. This executable: /fixture/bin/9l. First on PATH: /fixture/bin/9l.');
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

// spec/import reads local bindings. Only a `test` bound from @playwright/test
// with nothing else that could provide it is missing (exit 2); a test from a
// fixtures module is not followed, so it is a warning, never missing.
test('spec/import reads bindings and never fails a spec it cannot follow', () => {
  const root = specProject({
    'aliased-pw.spec.ts': poison + "import { test } from './fixtures';\nimport { test as pwTest, expect } from '@playwright/test';\ntest('x', async ({ page }) => {});\n",
    'only-alias.spec.ts': poison + "import { test as pwTest } from '@playwright/test';\npwTest('x', async ({ page }) => {});\n",
    'namespace.spec.ts': poison + "import * as n9l from '@9l/playwright';\nimport { test } from '@playwright/test';\nn9l.test('x', async ({ page }) => {});\n",
    'string.spec.ts': poison + "import { test } from './fixtures';\nconst snippet = \"import { test } from '@playwright/test'\";\nconst more = `const { test } = require('@playwright/test')`;\ntest('x', async ({ page }) => {});\n",
    'regex-quote.spec.ts': poison + "const quotes = /['\"]/;\n// import { test } from '@playwright/test'\nimport { test } from './fixtures';\ntest('x', async ({ page }) => {});\n",
    'base.spec.ts': poison + "import { test as base } from '@9l/playwright';\nconst test = base.extend({});\ntest('x', async ({ page }) => {});\n",
    'member.spec.js': poison + "const test = require('@9l/playwright').test;\ntest('x', async ({ page }) => {});\n",
    'dynamic.spec.mjs': poison + "const { test } = await import('@9l/playwright');\ntest('x', async ({ page }) => {});\n",
    'default.spec.ts': poison + "import n9l, { test } from '@9l/playwright';\ntest('x', async ({ page }) => {});\n",
    'type-only.spec.ts': poison + "import type { test } from '@playwright/test';\nimport { test as t } from './fixtures';\nt('x', async ({ page }) => {});\n",
    'local-ns.spec.ts': poison + "import * as fixtures from './fixtures';\nimport { test } from '@playwright/test';\nfixtures.test('x', async ({ page }) => {});\n",
    'plain.spec.ts': poison + "import { test } from '@playwright/test';\ntest('x', async ({ page }) => {});\n",
  });
  try {
    const status = name => byName(specChecks(path.join(root, name)), 'spec/import');
    for (const name of ['namespace.spec.ts', 'base.spec.ts', 'member.spec.js', 'dynamic.spec.mjs', 'default.spec.ts']) assert.equal(status(name).status, 'ok', name);
    for (const name of ['aliased-pw.spec.ts', 'only-alias.spec.ts', 'string.spec.ts', 'regex-quote.spec.ts', 'type-only.spec.ts', 'local-ns.spec.ts']) assert.equal(status(name).status, 'warning', name);
    assert.match(status('aliased-pw.spec.ts').message, /imports test from another module; fixture modules are not followed/);
    assert.match(byName(specChecks(path.join(root, 'aliased-pw.spec.ts'), true), 'spec/import').message, /imports test from \.\/fixtures;/);
    assert.match(status('local-ns.spec.ts').message, /also imports from another module, which is not followed/);
    const plain = status('plain.spec.ts');
    assert.equal(plain.status, 'missing');
    assert.match(plain.message, /prove would run its baseline uninstrumented and exit 1 because no browser context was instrumented/);
    assert.doesNotMatch(plain.message, /refuses/);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// Fixtures count only as the destructured first parameter of a test or hook
// callback, whatever follows it: more parameters, a type or nested patterns.
test('spec/fixtures reads test and hook callbacks with any parameter shape', () => {
  const root = specProject({
    'info.spec.ts': poison + "import { test } from '@9l/playwright';\ntest('x', async ({ page, n9l }, testInfo) => {});\n",
    'typed.spec.ts': poison + "import { test, type Page } from '@9l/playwright';\ntest('x', async ({ page }: { page: Page }) => {});\n",
    'nested.spec.ts': poison + "import { test } from '@9l/playwright';\ntest('x', async ({ request, context: { pages } }) => { pages(); });\n",
    'hook.spec.ts': poison + "import { test } from '@9l/playwright';\ntest.describe.serial('d', () => {\n  test.beforeEach(async function ({ context }) {});\n  test('x', async () => {});\n});\n",
    'alias.spec.ts': poison + "import { test as it } from '@9l/playwright';\nit(`t ${1}`, async ({ page }) => {});\n",
    'namespace.spec.ts': poison + "import * as n9l from '@9l/playwright';\nn9l.test.only('x', ({ page }) => {});\n",
    'map.spec.ts': poison + "import { test } from '@9l/playwright';\ntest('x', async ({ request }) => { [{ page: 1 }].map(({ page }) => page); const f = ({ context }) => context; });\n",
    'extend.spec.ts': poison + "import { test as base } from '@9l/playwright';\nconst test = base.extend({ login: async ({ page }, use) => { await use(page); } });\ntest('x', async ({ login }) => {});\n",
  });
  try {
    const fixtures = name => byName(specChecks(path.join(root, name)), 'spec/fixtures');
    for (const name of ['info.spec.ts', 'typed.spec.ts', 'nested.spec.ts', 'hook.spec.ts', 'alias.spec.ts', 'namespace.spec.ts']) assert.equal(fixtures(name).status, 'ok', name);
    for (const name of ['map.spec.ts', 'extend.spec.ts']) {
      assert.equal(fixtures(name).status, 'warning', name);
      assert.match(fixtures(name).message, /no destructured page or context parameter found in a test or hook callback/);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// Regex literals are not comments or strings: a `/*` or a quote inside one
// must not swallow the code after it.
test('regex literals do not start comments or strings', () => {
  const root = specProject({
    'glob.spec.ts': poison + "const api = /^\\/api\\/*/; import { test } from '@9l/playwright';\ntest('x', async ({ page }) => {});\n",
    'slashes.spec.ts': poison + "const re = /\\/\\//g, q = /['\"]/, cls = /[/*]/;\nimport { test } from '@9l/playwright';\ntest('x', async ({ page }) => {});\n",
    'divide.spec.ts': poison + "const half = (1) / 2, rate = total / count /* ratio */;\nimport { test } from '@9l/playwright';\ntest('x', async ({ page }) => {});\n",
  });
  try {
    for (const name of fs.readdirSync(root)) {
      const checks = specChecks(path.join(root, name));
      assert.equal(byName(checks, 'spec/import').status, 'ok', name);
      assert.equal(byName(checks, 'spec/fixtures').status, 'ok', name);
    }
    const { scan } = require('../dist/source-text.js');
    const source = "x = /a'b/; return /\\/\\*/.test(s) ? `${'//'}` : 'c' // d\n";
    const scanned = scan(source);
    assert.equal(scanned.code.length, source.length);
    assert.doesNotMatch(scanned.code, /\/\/ d/, 'the trailing comment is removed');
    assert.deepEqual([...scanned.literals.values()].map(literal => literal.text), ['//', 'c']);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// Projects inherit the config's testDir unless they set their own, so any
// config with projects leaves containment unverified rather than warning.
test('a config with projects leaves testDir unverified', () => {
  const root = specProject({
    'playwright.config.ts': poison + "export default { projects: [{ name: 'api', testDir: './api' }, { name: 'web' }] };\n",
    'tests/a.spec.ts': sdkSpec,
  });
  try {
    const config = byName(specChecks(path.join(root, 'tests', 'a.spec.ts')), 'spec/config');
    assert.equal(config.status, 'ok');
    assert.match(config.message, /testDir not verified: the config defines projects/);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// With a spec, workspace and packages are judged for the project prove uses,
// found by the adapter's walk up from the spec, not for the current directory.
test('spec mode judges the project prove would use for the spec', () => {
  const project = specProject({
    'package.json': JSON.stringify({ devDependencies: { '@playwright/test': '1.64.0' } }),
    'node_modules/@playwright/test/package.json': JSON.stringify({ name: '@playwright/test', version: '1.64.0' }),
    'node_modules/@playwright/test/index.js': poison,
    'tests/a.spec.ts': sdkSpec,
    'orphan/b.spec.ts': sdkSpec,
  });
  const elsewhere = fs.mkdtempSync(path.join(os.tmpdir(), '9l-doctor-cwd-'));
  try {
    const spec = path.join(project, 'tests', 'a.spec.ts');
    const report = diagnose({ cliVersion: '0.5.0', sdk: true, spec }, elsewhere);
    assert.equal(byName(report.checks, 'workspace').status, 'ok');
    assert.equal(byName(report.checks, 'playwright-test').status, 'ok');
    assert.doesNotMatch(JSON.stringify(report), /9l-doctor-spec|fixture-/);
    assert.ok(byName(diagnose({ cliVersion: '0.5.0', sdk: true, spec, paths: true }, elsewhere).checks, 'workspace').message.includes(project));
    assert.equal(byName(diagnose({ cliVersion: '0.5.0', sdk: true }, elsewhere).checks, 'workspace').status, 'missing', 'without a spec, the current directory');
    fs.writeFileSync(path.join(project, 'package.json'), '{}');
    const orphan = byName(diagnose({ cliVersion: '0.5.0', sdk: true, spec: path.join(project, 'orphan', 'b.spec.ts') }, elsewhere).checks, 'workspace');
    assert.equal(orphan.status, 'missing');
    assert.match(orphan.message, /above the spec, so prove cannot run it/);
  } finally {
    fs.rmSync(project, { recursive: true, force: true });
    fs.rmSync(elsewhere, { recursive: true, force: true });
  }
});

// A spec that takes the SDK under another name but registers tests with test
// from @playwright/test runs those tests uninstrumented, so it is not ok.
test('spec/import does not pass a spec that registers tests with the plain test', () => {
  const root = specProject({
    'expect-only.spec.ts': poison + "import * as n9l from '@9l/playwright';\nimport { test } from '@playwright/test';\ntest('x', async ({ page }) => { n9l.expect(page).toBeTruthy(); });\n",
    'default-sdk.spec.ts': poison + "import n9l from '@9l/playwright';\nimport { test } from '@playwright/test';\ntest.describe('d', () => { n9l.test('x', async ({ page }) => {}); });\n",
    'both-named.spec.ts': poison + "import { test } from '@9l/playwright';\nimport { test as pw } from '@playwright/test';\npw('x', async ({ page }) => {});\n",
    'plain-ns.spec.ts': poison + "import { test } from '@9l/playwright';\nimport * as pw from '@playwright/test';\npw.test('x', async ({ page }) => {});\n",
    'expect-from-plain.spec.ts': poison + "import { test } from '@9l/playwright';\nimport { expect } from '@playwright/test';\ntest('x', async ({ page }) => { expect(page).toBeTruthy(); });\n",
  });
  try {
    const status = name => byName(specChecks(path.join(root, name)), 'spec/import');
    for (const name of ['expect-only.spec.ts', 'default-sdk.spec.ts', 'both-named.spec.ts', 'plain-ns.spec.ts']) {
      assert.equal(status(name).status, 'warning', name);
      assert.match(status(name).message, /also registers tests with test from @playwright\/test, which prove does not instrument; which tests use which is not verified/, name);
    }
    assert.equal(status('expect-from-plain.spec.ts').status, 'ok', 'expect from @playwright/test registers nothing');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// A default import, or a namespace from a package, can provide the test a
// spec registers with; doctor does not follow it, so it is never missing.
test('spec/import never fails a spec whose test may come from a default or package import', () => {
  const root = specProject({
    'default-local.spec.ts': poison + "import { test } from '@playwright/test';\nimport fixtures from './fixtures';\nfixtures.test('x', async ({ page }) => {});\n",
    'default-called.spec.ts': poison + "import { test } from '@playwright/test';\nimport it from '@company/test-fixtures';\nit('x', async ({ page }) => {});\n",
    'package-ns.spec.ts': poison + "import { test } from '@playwright/test';\nimport * as fx from '@company/test-fixtures';\nfx.test('x', async ({ page }) => {});\n",
    'package-require.spec.cjs': poison + "const { test } = require('@playwright/test');\nconst fx = require('@company/test-fixtures');\nfx.test('x', async ({ page }) => {});\n",
    'helpers.spec.ts': poison + "import { test } from '@playwright/test';\nimport * as path from 'node:path';\nimport fs from 'node:fs';\ntest('x', async ({ page }) => { fs.readFileSync(path.join('a', 'b')); });\n",
  });
  try {
    const status = name => byName(specChecks(path.join(root, name)), 'spec/import');
    for (const name of ['default-local.spec.ts', 'default-called.spec.ts', 'package-ns.spec.ts', 'package-require.spec.cjs']) {
      assert.equal(status(name).status, 'warning', name);
      assert.match(status(name).message, /also imports from another module, which is not followed/, name);
    }
    assert.match(byName(specChecks(path.join(root, 'package-ns.spec.ts'), true), 'spec/import').message, /also imports from @company\/test-fixtures,/);
    assert.equal(status('helpers.spec.ts').status, 'missing', 'modules the spec never takes a test from do not hide the plain test');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// A named import used the way a test function is used, called with a title
// and a callback or through describe, a hook or extend, may be a test built
// from @9l/playwright in a module doctor does not follow: a warning, never
// missing. Helpers called any other way do not hide the plain test.
test('spec/import never fails a spec that registers tests through a named import from a fixtures module', () => {
  const plain = "import { test } from '@playwright/test';\n";
  const root = specProject({
    'it.spec.ts': poison + plain + "import { it } from './fixtures';\nit('x', async ({ page }) => { await page.goto('/'); });\n",
    'describe.spec.ts': poison + plain + "import { it } from './fixtures';\nit.describe('d', () => {\n  it.beforeEach(async ({ context }) => {});\n});\n",
    'serial.spec.ts': poison + plain + "import { it } from '../support/fixtures';\nit.describe.serial('d', () => {});\n",
    'extend.spec.ts': poison + plain + "import { base } from './fixtures';\nconst t = base.extend({});\nt('x', async ({ page }) => {});\n",
    'details.spec.ts': poison + plain + "import { it } from './fixtures';\nit('x', { tag: '@smoke' }, async function ({ page }) {});\n",
    'template.spec.ts': poison + plain + "import { it } from './fixtures';\n['a', 'b'].forEach(name => { it(`x ${name}`, async ({ page }) => {}); });\n",
    'only.spec.ts': poison + plain + "import { it } from './fixtures';\nit.only('x', async ({ page }) => {});\n",
    'required.spec.cjs': poison + "const { test } = require('@playwright/test');\nconst { it: check } = require('./fixtures');\ncheck('x', async ({ page }) => {});\n",
    'package.spec.ts': poison + plain + "import { it } from '@company/test-fixtures';\nit('x', async ({ page }) => {});\n",
    'alone.spec.ts': poison + "import { it } from './fixtures';\nit('x', async ({ page }) => {});\n",
    'both.spec.ts': poison + plain + "import { it } from './fixtures';\nit('x', async ({ page }) => {});\ntest('y', async ({ page }) => {});\n",
  });
  try {
    const checks = name => specChecks(path.join(root, name));
    for (const name of fs.readdirSync(root)) {
      const check = byName(checks(name), 'spec/import');
      assert.equal(check.status, 'warning', name);
      assert.match(check.message, /registers tests with \w+ from another module, which is not followed; whether those tests come from @9l\/playwright is not verified/, name);
    }
    assert.match(byName(checks('it.spec.ts'), 'spec/import').message, /^it\.spec\.ts imports test from @playwright\/test but registers tests with it from another module/);
    assert.match(byName(specChecks(path.join(root, 'serial.spec.ts'), true), 'spec/import').message, /with it from \.\.\/support\/fixtures,/);
    assert.match(byName(checks('required.spec.cjs'), 'spec/import').message, /with check from another module/);
    assert.match(byName(checks('both.spec.ts'), 'spec/import').message, /Tests the spec registers with the plain test from @playwright\/test are not instrumented\.$/);
    assert.doesNotMatch(byName(checks('it.spec.ts'), 'spec/import').message, /plain test/);
    for (const name of ['it.spec.ts', 'describe.spec.ts', 'details.spec.ts', 'template.spec.ts', 'only.spec.ts', 'required.spec.cjs', 'alone.spec.ts']) {
      assert.equal(byName(checks(name), 'spec/fixtures').status, 'ok', `${name}: callbacks of a fixtures-module test count`);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('spec/import still fails a plain spec that only calls helpers from a local module', () => {
  const plain = "import { test } from '@playwright/test';\n";
  const root = specProject({
    'login.spec.ts': poison + plain + "import { login } from './helpers';\ntest('x', async ({ page }) => { await login(page); });\n",
    'titled.spec.ts': poison + plain + "import { login, fill } from './helpers';\ntest('x', async ({ page }) => { await login('admin', page); await fill('#q', 'text'); });\nlogin('top level', 'no callback');\n",
    'step.spec.ts': poison + plain + "import { step } from './helpers';\ntest('x', async ({ page }) => {\n  await step('fill the form', async () => { await page.goto('/'); });\n});\ntest.describe('d', () => {\n  test.beforeEach(async () => { await step('seed', async () => {}); });\n});\n",
    'members.spec.ts': poison + plain + "import { api, logger, server } from './helpers';\nserver.use(handler);\nlogger.info('x', () => 1);\ntest('x', async ({ page }) => { await api.login(page); api.only('y'); });\n",
    'expect.spec.ts': poison + plain + "import { expect } from './fixtures';\nexpect.extend({ toBeShown() { return { pass: true }; } });\ntest('x', async ({ page }) => { expect(page).toBeTruthy(); });\n",
    'comment.spec.ts': poison + plain + "import { it } from './fixtures';\n// it('x', async ({ page }) => {});\nconst s = \"it.describe('d', () => {})\";\ntest('x', async ({ page }) => {});\n",
    'member-of.spec.ts': poison + plain + "import { it } from './fixtures';\nconst o = { it };\no.it('x', async () => {});\nx.it.describe('d', () => {});\ntest('x', async ({ page }) => {});\n",
  });
  try {
    for (const name of fs.readdirSync(root)) {
      const check = byName(specChecks(path.join(root, name)), 'spec/import');
      assert.equal(check.status, 'missing', name);
      assert.equal(check.fix, 'import test from @9l/playwright; prove instruments only its page and context fixtures', name);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// A local alias or extension of the plain test is the plain test: a step
// helper inside its callback is inside a test body, not a test provider.
test('spec/import still fails a spec whose plain test is aliased or extended locally', () => {
  const plain = "import { test } from '@playwright/test';\nimport { step } from './helpers';\n";
  const inside = "('x', async ({ page }) => { await step('fill', async () => {}); });\n";
  const root = specProject({
    'extended.spec.ts': poison + plain + 'const t = test.extend({});\nt' + inside,
    'chained.spec.ts': poison + plain + "const t = test.extend({ a: async ({}, use) => { await use(1); } })\n  .extend<{ b: number }>({ b: 2 });\nt" + inside,
    'alias.spec.ts': poison + plain + 'const t = test;\nt' + inside,
    'typed.spec.ts': poison + plain + 'const t: typeof test = test\nt' + inside,
    'twice.spec.ts': poison + plain + 'const base = test.extend({});\nexport const t = base.extend({});\nt.describe(\'d\', () => {\n  t' + inside + '});\n',
    'namespace.spec.ts': poison + "import * as pw from '@playwright/test';\nimport { step } from './helpers';\nconst t = pw.test.extend({});\nt" + inside,
  });
  try {
    for (const name of fs.readdirSync(root)) {
      const checks = specChecks(path.join(root, name));
      assert.equal(byName(checks, 'spec/import').status, name === 'namespace.spec.ts' ? 'warning' : 'missing', name);
      assert.doesNotMatch(byName(checks, 'spec/import').message, /registers tests with step/, name);
      assert.equal(byName(checks, 'spec/fixtures').status, 'ok', `${name}: callbacks of an aliased test count`);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// A member access is not a call of the import, however it is spaced.
test('spec/import still fails a plain spec whose member call shares a fixtures import name', () => {
  const plain = "import { test } from '@playwright/test';\nimport { it } from './fixtures';\n";
  const tail = "test('y', async ({ page }) => {});\n";
  const root = specProject({
    'spaced.spec.ts': poison + plain + "o . it('x', async () => {});\n" + tail,
    'newline.spec.ts': poison + plain + "o\n  .it('x', async () => {});\n" + tail,
    'after-dot.spec.ts': poison + plain + "o.\n  it('x', async () => {});\n" + tail,
    'comment.spec.ts': poison + plain + "o./* member */it('x', async () => {});\no // note\n  .it.describe('d', () => {});\n" + tail,
    'optional.spec.ts': poison + plain + "o?. it('x', async () => {});\no ?.it.extend({});\n" + tail,
  });
  try {
    for (const name of fs.readdirSync(root)) {
      assert.equal(byName(specChecks(path.join(root, name)), 'spec/import').status, 'missing', name);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// A callback may be a function the spec declares and passes by name.
test('spec/import never fails a spec that passes a declared function to a fixtures-module test', () => {
  const plain = "import { test } from '@playwright/test';\nimport { it } from './fixtures';\n";
  const root = specProject({
    'arrow.spec.ts': poison + plain + "const body = async ({ page }) => { await page.goto('/'); };\nit('x', body);\n",
    'expression.spec.ts': poison + plain + "const body = async function ({ context }) {};\nit('x', { tag: '@smoke' }, body);\n",
    'declaration.spec.ts': poison + plain + "it.only('x', body);\nasync function body({ page }: { page: Page }) {}\n",
    'typed.spec.ts': poison + plain + "const body: Body = async ({ page }): Promise<void> => {};\nit(`x`, body);\n",
  });
  try {
    for (const name of fs.readdirSync(root)) {
      const checks = specChecks(path.join(root, name));
      assert.equal(byName(checks, 'spec/import').status, 'warning', name);
      assert.match(byName(checks, 'spec/import').message, /registers tests with it from another module/, name);
      assert.equal(byName(checks, 'spec/fixtures').status, 'ok', `${name}: a referenced callback's fixtures count`);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// Only an unambiguous function declared in the spec counts; values, unknown
// names, redeclared names and helpers inside a test body do not.
test('spec/import still fails a plain spec that passes values or helpers by name', () => {
  const plain = "import { test } from '@playwright/test';\nimport { it, step } from './fixtures';\n";
  const tail = "test('y', async ({ page }) => {});\n";
  const root = specProject({
    'value.spec.ts': poison + plain + "const body = { page: 1 };\nit('x', body);\n" + tail,
    'unknown.spec.ts': poison + plain + "it('x', page);\n" + tail,
    'paren.spec.ts': poison + plain + "const body = (1, 2);\nit('x', body);\n" + tail,
    'redeclared.spec.ts': poison + plain + "function body() {}\nlet body2 = 1;\n{ const body = 2; }\nit('x', body);\n" + tail,
    'imported.spec.ts': poison + plain + "import { body } from './helpers';\nit('x', body);\n" + tail,
    'step.spec.ts': poison + plain + "const fill = async () => {};\ntest('x', async ({ page }) => { await step('fill', fill); });\n",
  });
  try {
    for (const name of fs.readdirSync(root)) {
      assert.equal(byName(specChecks(path.join(root, name)), 'spec/import').status, 'missing', name);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// A type annotation between the name and `=` may hold arrow types, generics
// and object types; the callback it types is still a declared function.
test('spec/import reads a declared callback or alias whose binding has a type annotation', () => {
  const plain = "import { test } from '@playwright/test';\nimport { it } from './fixtures';\n";
  const root = specProject({
    'arrow-type.spec.ts': poison + plain + "const body: (fixtures: { page: unknown }) => Promise<void> = async ({ page }) => {};\nit('x', body);\n",
    'generic-type.spec.ts': poison + plain + "const body: Callback<{ page: Page }, Promise<void>> = async ({ page }) => {};\nit('x', body);\n",
    'object-type.spec.ts': poison + plain + "const body: { (fixtures: { page: Page }): Promise<void> } = async function ({ page }) {};\nit('x', body);\n",
    'default-type.spec.ts': poison + plain + "const body: Callback<{ page: Page }, (x?: number) => void> = async ({ page }): Promise<void> => {};\nit('x', body);\n",
  });
  try {
    for (const name of fs.readdirSync(root)) {
      const checks = specChecks(path.join(root, name));
      assert.equal(byName(checks, 'spec/import').status, 'warning', name);
      assert.match(byName(checks, 'spec/import').message, /registers tests with it from another module/, name);
      assert.equal(byName(checks, 'spec/fixtures').status, 'ok', `${name}: an annotated callback's fixtures count`);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
  // An annotated local extension of the plain test is still the plain test.
  const step = "import { test } from '@playwright/test';\nimport { step } from './helpers';\n";
  const aliased = specProject({
    'typed.spec.ts': poison + step + "const t: TestType<{ run: (x: number) => void }, {}> = test.extend({});\nt('x', async ({ page }) => { await step('fill', async () => {}); });\n",
  });
  try {
    assert.equal(byName(specChecks(path.join(aliased, 'typed.spec.ts')), 'spec/import').status, 'missing');
  } finally { fs.rmSync(aliased, { recursive: true, force: true }); }
});

// A named function expression's own name is bound only inside it: it neither
// makes its outer binding ambiguous nor declares a function outside.
test('spec/import reads a function expression by its binding, not its own name', () => {
  const plain = "import { test } from '@playwright/test';\nimport { it } from './fixtures';\n";
  const root = specProject({
    'self-named.spec.ts': poison + plain + "const body = async function body({ page }) {};\nit('x', body);\n",
    'self-named-generator.spec.ts': poison + plain + "const body = function* body({ page }) {};\nit('x', body);\n",
  });
  try {
    for (const name of fs.readdirSync(root)) {
      const checks = specChecks(path.join(root, name));
      assert.equal(byName(checks, 'spec/import').status, 'warning', name);
      assert.equal(byName(checks, 'spec/fixtures').status, 'ok', `${name}: a self-named callback's fixtures count`);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
  const tail = "test('y', async ({ page }) => {});\n";
  const hidden = specProject({
    'inner-name.spec.ts': poison + plain + "const run = async function body({ page }) {};\nit('x', body);\n" + tail,
    'argument-name.spec.ts': poison + plain + "wrap(function body({ page }) {});\nit('x', body);\n" + tail,
  });
  try {
    for (const name of fs.readdirSync(hidden)) {
      assert.equal(byName(specChecks(path.join(hidden, name)), 'spec/import').status, 'missing', name);
    }
  } finally { fs.rmSync(hidden, { recursive: true, force: true }); }
});

// A function passed by name to the plain test, its aliases or a hook is a
// test body: a titled helper call inside it is not a top-level test provider.
test('spec/import still fails a plain spec whose named test or hook callback calls a step helper', () => {
  const plain = "import { test } from '@playwright/test';\nimport { step } from './helpers';\n";
  const filled = "async ({ page }) => { await step('fill', async () => {}); }";
  const root = specProject({
    'arrow.spec.ts': poison + plain + `const body = ${filled};\ntest('x', body);\n`,
    'declaration.spec.ts': poison + plain + "test('x', { tag: '@a' }, body);\nasync function body({ page }) { await step('fill', async () => {}); }\n",
    'expression.spec.ts': poison + plain + "const body = async ({ page }) => step('fill', async () => {});\ntest('x', body);\n",
    'hook.spec.ts': poison + plain + "const seed = async () => { await step('seed', async () => {}); };\ntest.beforeEach(seed);\ntest('x', async ({ page }) => {});\n",
    'nested-hook.spec.ts': poison + plain + "const seed = async () => { await step('seed', async () => {}); };\ntest.describe('d', () => {\n  test.afterAll(seed);\n  test('x', async ({ page }) => {});\n});\n",
    'alias.spec.ts': poison + plain + `const t = test.extend({});\nconst body = ${filled};\nt.only('x', body);\n`,
  });
  try {
    for (const name of fs.readdirSync(root)) {
      const checks = specChecks(path.join(root, name));
      assert.equal(byName(checks, 'spec/import').status, 'missing', name);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
  // A describe callback registers tests: it is not a test body, so a fixtures
  // test called in it is still a possible provider.
  const suites = specProject({
    'suite.spec.ts': poison + "import { test } from '@playwright/test';\nimport { it } from './fixtures';\nconst suite = () => { it('x', async ({ page }) => {}); };\ntest.describe('d', suite);\n",
  });
  try {
    assert.equal(byName(specChecks(path.join(suites, 'suite.spec.ts')), 'spec/import').status, 'warning');
  } finally { fs.rmSync(suites, { recursive: true, force: true }); }
});

// Doctor reads a call's arguments, a type annotation and a parameter list
// within a bound. A spec it reads only in part is not verified: the call may
// register a test, so spec/import is a warning, never missing. A block
// comment of `length` characters stands in for any long text.
const comment = length => `/*${'c'.repeat(length - 4)}*/`;
test('spec/import treats a test call doctor reads only in part as a possible registration', () => {
  const plain = "import { test } from '@playwright/test';\nimport { it } from './fixtures';\n";
  const tail = "test('y', async ({ page }) => {});\n";
  // `('x', ` takes 6 characters from the `(`, the callback 22 and `)` 1, so
  // with a 4,067-character comment the call ends on the last character read.
  const titled = length => `it('x', ${comment(length)}async ({ page }) => {});\n`;
  const root = specProject({
    'complete-4067.spec.ts': poison + plain + titled(4067) + tail,
    'cut-after-callback-4068.spec.ts': poison + plain + titled(4068) + tail,
    'cut-in-callback-4089.spec.ts': poison + plain + titled(4089) + tail,
    'cut-before-callback-4090.spec.ts': poison + plain + titled(4090) + tail,
    'cut-before-callback-5000.spec.ts': poison + plain + titled(5000) + tail,
    'cut-before-title.spec.ts': poison + plain + `it(${comment(5000)}'x', async ({ page }) => {});\n` + tail,
    'cut-in-details.spec.ts': poison + plain + `it('x', { tag: '@a', ${comment(5000)} }, async ({ page }) => {});\n` + tail,
  });
  try {
    for (const name of fs.readdirSync(root)) {
      const check = byName(specChecks(path.join(root, name)), 'spec/import');
      assert.equal(check.status, 'warning', name);
      assert.match(check.message, /registers tests with it from another module/, name);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
  // Reading in part does not turn a helper into a test: a cut-off step
  // wrapper in a test body, an untitled call, or a fixtures name never
  // called, still leaves the plain spec missing.
  const step = "import { test } from '@playwright/test';\nimport { step, login, it } from './helpers';\n";
  const helpers = specProject({
    'step.spec.ts': poison + step + `test('x', async ({ page }) => { await step('fill', ${comment(5000)}async () => {}); });\n`,
    'untitled.spec.ts': poison + step + `login(page, ${comment(5000)}async () => {});\n` + tail,
    'plain-cut.spec.ts': poison + step + `test('x', ${comment(5000)}async ({ page }) => { it; });\n`,
  });
  try {
    for (const name of fs.readdirSync(helpers)) {
      assert.equal(byName(specChecks(path.join(helpers, name)), 'spec/import').status, 'missing', name);
    }
  } finally { fs.rmSync(helpers, { recursive: true, force: true }); }
});

// A callback declared with an annotation, a parameter list or a gap doctor
// reads only in part may still be a function passed to a fixtures test.
test('spec/import treats a declared callback doctor reads only in part as a possible callback', () => {
  const plain = "import { test } from '@playwright/test';\nimport { it } from './fixtures';\n";
  const call = "it('x', body);\n";
  const root = specProject({
    'annotation-1000.spec.ts': poison + plain + `const body: ${comment(1000)}Body = async ({ page }) => {};\n` + call,
    'annotation-1100.spec.ts': poison + plain + `const body: ${comment(1100)}Body = async ({ page }) => {};\n` + call,
    'after-equals.spec.ts': poison + plain + `const body = ${comment(100)}async ({ page }) => {};\n` + call,
    'parameters.spec.ts': poison + plain + `const body = async ({ page, ${comment(5000)} }) => {};\n` + call,
  });
  try {
    for (const name of fs.readdirSync(root)) {
      assert.equal(byName(specChecks(path.join(root, name)), 'spec/import').status, 'warning', name);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// Type arguments may nest angle brackets, parentheses, braces, brackets and
// arrow types: `base.extend<{ values: Array<string> }>(…)`.
test('spec/import reads typed extensions of a fixtures test and of the plain test', () => {
  const plain = "import { test } from '@playwright/test';\n";
  const typed = ['{ values: Array<string> }', '{ getValue: () => string }', '{ map: Map<string, [number, (x: { a: 1 }) => void]> }'];
  const root = specProject(Object.fromEntries(typed.map((type, k) => [`base-${k}.spec.ts`,
    poison + plain + `import { base } from './fixtures';\nconst t = base.extend<${type}>({});\nt('x', async ({ page }) => {});\n`])));
  try {
    for (const name of fs.readdirSync(root)) {
      const checks = specChecks(path.join(root, name));
      assert.equal(byName(checks, 'spec/import').status, 'warning', name);
      assert.match(byName(checks, 'spec/import').message, /registers tests with base from another module/, name);
      assert.equal(byName(checks, 'spec/fixtures').status, 'ok', name);
    }
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
  // A typed extension of the plain test is the plain test: a step helper in
  // its callback is inside a test body.
  const step = plain + "import { step } from './helpers';\n";
  const inside = "('x', async ({ page }) => { await step('fill', async () => {}); });\n";
  const aliased = specProject({
    ...Object.fromEntries(typed.map((type, k) => [`plain-${k}.spec.ts`, poison + step + `const t = test.extend<${type}>({});\nt` + inside])),
    'chained.spec.ts': poison + step + "const t = test.extend<{ a: () => void }>({})\n  .extend<{ b: Array<() => string> }>({});\nt" + inside,
  });
  try {
    for (const name of fs.readdirSync(aliased)) {
      const checks = specChecks(path.join(aliased, name));
      assert.equal(byName(checks, 'spec/import').status, 'missing', name);
      assert.equal(byName(checks, 'spec/fixtures').status, 'ok', name);
    }
  } finally { fs.rmSync(aliased, { recursive: true, force: true }); }
  // Beside the SDK, a typed extension of the plain test still registers
  // uninstrumented tests.
  const mixed = specProject({
    'mixed.spec.ts': poison + "import { test } from '@9l/playwright';\nimport { test as pw } from '@playwright/test';\nconst t = pw.extend<{ getValue: () => string }>({});\nt('x', async ({ page }) => {});\n",
  });
  try {
    const check = byName(specChecks(path.join(mixed, 'mixed.spec.ts')), 'spec/import');
    assert.equal(check.status, 'warning');
    assert.match(check.message, /also registers tests with test from @playwright\/test/);
  } finally { fs.rmSync(mixed, { recursive: true, force: true }); }
  // Type arguments longer than doctor reads are not verified either way: a
  // fixtures extension may register tests, and a plain extension is not
  // proven to be the plain test.
  const long = `{ ${comment(2000)} a: Array<string> }`;
  const cut = specProject({
    'base.spec.ts': poison + plain + `import { base } from './fixtures';\nconst t = base.extend<${long}>({});\nt('x', async ({ page }) => {});\n`,
    'plain.spec.ts': poison + step + `const t = test.extend<${long}>({});\nt` + inside,
  });
  try {
    for (const name of fs.readdirSync(cut)) {
      assert.equal(byName(specChecks(path.join(cut, name)), 'spec/import').status, 'warning', name);
    }
  } finally { fs.rmSync(cut, { recursive: true, force: true }); }
});

// projects and spreads can change which testDir applies, however written.
test('spec/config leaves testDir unverified for shorthand projects, spreads and merges', () => {
  const config = body => poison + "import { defineConfig, devices } from '@playwright/test';\nimport shared from './shared';\n" + body;
  const root = specProject({
    'shorthand/playwright.config.ts': config("import { projects } from './projects';\nexport default defineConfig({ testDir: './api', projects });\n"),
    'assigned/playwright.config.ts': config("const config = { testDir: './api' };\nconfig.projects = shared;\nexport default config;\n"),
    'spread-after/playwright.config.ts': config("export default defineConfig({ testDir: './api', ...shared });\n"),
    'spread-before/playwright.config.ts': config("export default defineConfig({ ...shared, testDir: './api' });\n"),
    'merged/playwright.config.ts': config("export default defineConfig(shared, { testDir: './api' });\n"),
    'spread-args/playwright.config.ts': config("export default defineConfig(...[shared], { testDir: './api' });\n"),
    'assign/playwright.config.ts': config("module.exports = Object.assign({ testDir: './api' }, shared);\n"),
    'use-spread/playwright.config.ts': config("export default defineConfig({ testDir: './api', reporter: [...shared.reporters], use: { ...devices['Desktop Chrome'], baseURL: 'http://localhost' }, });\n"),
  });
  for (const name of fs.readdirSync(root)) {
    fs.mkdirSync(path.join(root, name, 'web'));
    fs.writeFileSync(path.join(root, name, 'web', 'a.spec.ts'), sdkSpec);
  }
  try {
    const config = name => byName(specChecks(path.join(root, name, 'web', 'a.spec.ts')), 'spec/config');
    for (const name of ['shorthand', 'assigned']) {
      assert.equal(config(name).status, 'ok', name);
      assert.match(config(name).message, /testDir not verified: the config defines projects/, name);
    }
    for (const name of ['spread-after', 'spread-before', 'merged', 'spread-args', 'assign']) {
      assert.equal(config(name).status, 'ok', name);
      assert.match(config(name).message, /testDir not verified: the config spreads or merges values that can set testDir or projects/, name);
    }
    // A spread into use or an array sets neither, so the literal still applies.
    const verified = config('use-spread');
    assert.equal(verified.status, 'warning');
    assert.match(verified.message, /outside its literal testDir/);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});
