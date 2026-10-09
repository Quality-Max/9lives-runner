// Point every workspace and fixture pin at one @playwright/test release, or
// check that each of them resolves exactly that release. CI uses this to
// qualify releases in the SDK peer range beyond the workspace pin; a fixture
// that silently kept its own nested copy would otherwise qualify nothing.
//
//   node scripts/select-playwright.cjs <version>          rewrite the pins
//   node scripts/select-playwright.cjs --check <version>  verify resolution
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..');
const manifests = ['package.json', 'testdata/sdk/package.json', 'testdata/assessment/package.json', 'testdata/playwright/package.json'];
// Directories whose own resolution of @playwright/test must match.
const consumers = ['.', 'packages/playwright', 'testdata/sdk', 'testdata/playwright'];

function usage() {
  console.error('usage: select-playwright.cjs [--check] <major.minor.patch>');
  process.exit(2);
}

const args = process.argv.slice(2);
const check = args[0] === '--check';
if (check) args.shift();
if (args.length !== 1 || !/^\d+\.\d+\.\d+$/.test(args[0])) usage();
const version = args[0];
const [major, minor, patch] = version.split('.').map(Number);
// The SDK peer range is >=1.61.1 <2.
if (major !== 1 || minor < 61 || (minor === 61 && patch < 1)) {
  console.error(`${version} is outside the SDK peer range >=1.61.1 <2`);
  process.exit(2);
}

function installed(name, from) {
  return JSON.parse(fs.readFileSync(require.resolve(`${name}/package.json`, {paths: [from]}), 'utf8')).version;
}

if (check) {
  for (const consumer of consumers) {
    const dir = path.join(root, consumer);
    const test = installed('@playwright/test', dir);
    assert.equal(test, version, `${consumer} resolves @playwright/test ${test}`);
    // @playwright/test re-exports the playwright package it depends on.
    const core = installed('playwright', path.dirname(require.resolve('@playwright/test/package.json', {paths: [dir]})));
    assert.equal(core, version, `${consumer} resolves playwright ${core}`);
  }
  console.log(`Playwright ${version} resolved from ${consumers.join(', ')}`);
} else {
  for (const manifest of manifests) {
    const file = path.join(root, manifest);
    const pkg = JSON.parse(fs.readFileSync(file, 'utf8'));
    assert(pkg.devDependencies?.['@playwright/test'], `${manifest} has no @playwright/test pin`);
    pkg.devDependencies['@playwright/test'] = version;
    fs.writeFileSync(file, `${JSON.stringify(pkg, null, 2)}\n`);
  }
  console.log(`Pinned @playwright/test ${version} in ${manifests.join(', ')}`);
}
