const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {spawnSync} = require('node:child_process');
const {semver, awaitVersion} = require('./sdk-registry.cjs');

// Verify an SDK release on either trigger: a pushed sdk-v<version> tag, or a
// push to main whose manifest version is not on the registry yet. Both must
// be on public main history. Outputs `version` and `publish` for the workflow.
async function main() {
  const root = path.resolve(__dirname, '..');
  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'packages/playwright/package.json')));
  const version = manifest.version;
  assert.match(version, semver);
  assert.equal(process.env.REPOSITORY_PRIVATE, 'false');
  assert.equal(manifest.name, '@9l/playwright');
  assert.equal(manifest.license, 'Apache-2.0');
  assert.equal(manifest.repository.url, 'git+https://github.com/Quality-Max/9lives-runner.git');
  assert.notEqual(manifest.private, true);
  if (process.env.GITHUB_REF_TYPE === 'tag') {
    assert.equal(process.env.GITHUB_REF_NAME, `sdk-v${version}`);
  } else {
    assert.equal(process.env.GITHUB_REF_TYPE, 'branch');
    assert.equal(process.env.GITHUB_REF_NAME, 'main');
  }
  // A version already on the registry is not published again, whether the
  // trigger is a push to main or a tag pushed by hand after an automatic
  // publication whose tag step could not create the tag. The automatic tag
  // follows publication closely, so a tag run looks for up to five minutes
  // before concluding the version is unpublished.
  const tagged = process.env.GITHUB_REF_TYPE === 'tag';
  const publish = !(await awaitVersion(version, tagged ? {attempts: 20, intervalMs: 15000} : {attempts: 1, intervalMs: 0}));
  const ancestor = spawnSync('git', ['merge-base', '--is-ancestor', 'HEAD', 'origin/main'], {cwd: root, stdio: 'ignore'});
  assert.equal(ancestor.status, 0);
  if (process.env.GITHUB_OUTPUT) fs.appendFileSync(process.env.GITHUB_OUTPUT, `version=${version}\npublish=${publish}\n`);
  console.log(publish ? `SDK release ${version}, public repository and main ancestry verified` : `SDK ${version} is already published; nothing to do`);
}

main().catch(() => {
  console.error('SDK release rejected: require an unpublished version on public main, or a matching sdk-v<version> tag on main history');
  process.exitCode = 1;
});
