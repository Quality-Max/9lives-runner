const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {spawnSync} = require('node:child_process');
const {semver, releaseVersion} = require('./release-version.cjs');

// Verify a release tag before anything is built or published. One
// v<version> tag releases the CLI and @9l/playwright together, so it must
// equal every declared version. Outputs `version` for the workflow.
try {
  const root = path.resolve(__dirname, '..');
  const tag = process.env.GITHUB_REF_NAME;
  assert.equal(process.env.GITHUB_REF_TYPE, 'tag');
  assert.equal(process.env.GITHUB_REPOSITORY, 'Quality-Max/9lives-runner');
  assert.equal(process.env.REPOSITORY_PRIVATE, 'false');
  assert.match(tag, /^v/);
  assert.match(tag.slice(1), semver);
  const version = releaseVersion(root);
  assert.equal(tag, `v${version}`);
  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'packages/playwright/package.json')));
  assert.equal(manifest.name, '@9l/playwright');
  assert.equal(manifest.license, 'Apache-2.0');
  assert.equal(manifest.repository.url, 'git+https://github.com/Quality-Max/9lives-runner.git');
  assert.notEqual(manifest.private, true);
  const ancestor = spawnSync('git', ['merge-base', '--is-ancestor', 'HEAD', 'origin/main'], {cwd: root, stdio: 'ignore'});
  assert.equal(ancestor.status, 0);
  if (process.env.GITHUB_OUTPUT) fs.appendFileSync(process.env.GITHUB_OUTPUT, `version=${version}\n`);
  console.log(`Release ${version}: tag, CLI and SDK versions, public repository and main ancestry verified`);
} catch {
  console.error('Release rejected: require a v<major.minor.patch> tag on public main history that equals the CLI and SDK versions');
  process.exitCode = 1;
}
