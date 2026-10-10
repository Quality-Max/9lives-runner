// Publish only the exact tarball qualified by CI in this workflow run.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const {spawnSync} = require('node:child_process');
const {name, registryHas} = require('./sdk-registry.cjs');

try {
  const root = path.resolve(__dirname, '..');
  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'packages/playwright/package.json')));
  const directory = path.join(root, '.context/npm-pack');
  const qualification = JSON.parse(fs.readFileSync(path.join(directory, 'qualification.json')));
  assert.equal(qualification.name, manifest.name);
  assert.equal(qualification.version, manifest.version);
  const files = fs.readdirSync(directory).filter(file => file.endsWith('.tgz'));
  assert.deepEqual(files, [`9l-playwright-${manifest.version}.tgz`]);
  const tarball = path.join(directory, files[0]);
  assert.equal(crypto.createHash('sha256').update(fs.readFileSync(tarball)).digest('hex'), qualification.sha256);
  assert.deepEqual(qualification.results, [
    {spec: 'checkout', adapter: 'sdk', status: 'passed', validated: true, executedTests: 1},
    {spec: 'business-failure', adapter: 'sdk', status: 'failed', validated: true, executedTests: 1},
    {spec: 'ordinary', adapter: 'ordinary', status: 'passed', validated: true, executedTests: 1},
  ]);
  // A rerun after a successful publication skips, but only when npm holds
  // exactly these bytes: the same version from other source is an error.
  if (registryHas(manifest.version)) {
    const integrity = `sha512-${crypto.createHash('sha512').update(fs.readFileSync(tarball)).digest('base64')}`;
    const view = spawnSync('npm', ['view', `${name}@${manifest.version}`, 'dist.integrity'], {cwd: root, encoding: 'utf8', timeout: 60000});
    assert(!view.error && view.status === 0 && view.stdout.trim() === integrity, 'npm already has this version with different contents');
    console.log(`${name}@${manifest.version} is already published with identical contents; nothing to do`);
    return;
  }
  const result = spawnSync('npm', ['publish', tarball, '--access', 'public'], {cwd: root, stdio: 'inherit', timeout: 120000});
  assert(!result.error && result.status === 0);
} catch {
  console.error('SDK publication failed or qualified artifact was rejected');
  process.exitCode = 1;
}
