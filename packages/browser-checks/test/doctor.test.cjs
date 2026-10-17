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
