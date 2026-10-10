const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {spawnSync} = require('node:child_process');
const {releaseVersion} = require('./release-version.cjs');
const {registryHas} = require('./sdk-registry.cjs');

// Verify an SDK release for a pushed v<version> tag, the same tag that
// releases the CLI. Outputs `version` and `publish` for the workflow.
try {
  const root = path.resolve(__dirname, '..');
  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'packages/playwright/package.json')));
  const version = releaseVersion(root);
  assert.equal(process.env.REPOSITORY_PRIVATE, 'false');
  assert.equal(manifest.name, '@9l/playwright');
  assert.equal(manifest.license, 'Apache-2.0');
  assert.equal(manifest.repository.url, 'git+https://github.com/Quality-Max/9lives-runner.git');
  assert.notEqual(manifest.private, true);
  assert.equal(process.env.GITHUB_REF_TYPE, 'tag');
  assert.equal(process.env.GITHUB_REF_NAME, `v${version}`);
  const ancestor = spawnSync('git', ['merge-base', '--is-ancestor', 'HEAD', 'origin/main'], {cwd: root, stdio: 'ignore'});
  assert.equal(ancestor.status, 0);
  // A rerun after a successful publication finds the version and skips.
  const publish = !registryHas(version);
  if (process.env.GITHUB_OUTPUT) fs.appendFileSync(process.env.GITHUB_OUTPUT, `version=${version}\npublish=${publish}\n`);
  console.log(publish ? `SDK release ${version}, public repository and main ancestry verified` : `SDK ${version} is already published; nothing to do`);
} catch {
  console.error('SDK release rejected: require a v<version> tag on public main history whose CLI and SDK versions match it');
  process.exitCode = 1;
}
