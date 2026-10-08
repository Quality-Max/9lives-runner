const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {spawnSync} = require('node:child_process');

try {
  const root = path.resolve(__dirname, '..');
  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'packages/playwright/package.json')));
  assert.equal(process.env.GITHUB_REF_TYPE, 'tag');
  assert.equal(process.env.GITHUB_REF_NAME, `sdk-v${manifest.version}`);
  assert.equal(process.env.REPOSITORY_PRIVATE, 'false');
  assert.equal(manifest.name, '@9l/playwright');
  assert.equal(manifest.license, 'Apache-2.0');
  assert.equal(manifest.repository.url, 'git+https://github.com/Quality-Max/9lives-runner.git');
  assert.notEqual(manifest.private, true);
  const ancestor = spawnSync('git', ['merge-base', '--is-ancestor', 'HEAD', 'origin/main'], {cwd: root, stdio: 'ignore'});
  assert.equal(ancestor.status, 0);
  console.log('SDK release tag, public repository and main ancestry verified');
} catch {
  console.error('SDK release rejected: require matching sdk-v<version> tag on public main history');
  process.exitCode = 1;
}
