const assert = require('node:assert/strict');
const test = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { diagnose } = require('../dist/doctor.js');

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
