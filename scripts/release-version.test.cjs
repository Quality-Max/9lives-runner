const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const {declaredVersions, releaseVersion} = require('./release-version.cjs');

const root = path.resolve(__dirname, '..');

test('the CLI, SDK, lockfile and SDK fixture declare one release version', () => {
  const versions = declaredVersions();
  assert.equal(releaseVersion(), versions.cli, JSON.stringify(versions));
});

test('a drifted declaration is named', () => {
  const copy = fs.mkdtempSync(path.join(os.tmpdir(), 'release-version-'));
  try {
    for (const file of ['cmd/9l/main.go', 'packages/playwright/package.json', 'package-lock.json', 'testdata/sdk/package.json']) {
      fs.mkdirSync(path.dirname(path.join(copy, file)), {recursive: true});
      fs.copyFileSync(path.join(root, file), path.join(copy, file));
    }
    const manifest = path.join(copy, 'packages/playwright/package.json');
    const sdk = JSON.parse(fs.readFileSync(manifest, 'utf8'));
    sdk.version = '9.9.9';
    fs.writeFileSync(manifest, JSON.stringify(sdk));
    assert.throws(() => releaseVersion(copy), /sdk 9\.9\.9/);
  } finally {
    fs.rmSync(copy, {recursive: true, force: true});
  }
});
