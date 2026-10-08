const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {spawnSync} = require('node:child_process');

try {
  const root = path.resolve(__dirname, '..');
  const tag = process.env.GITHUB_REF_NAME;
  assert.equal(process.env.GITHUB_REF_TYPE, 'tag');
  assert.equal(process.env.GITHUB_REPOSITORY, 'Quality-Max/9lives-runner');
  assert.equal(process.env.REPOSITORY_PRIVATE, 'false');
  assert.match(tag, /^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/);
  const source = fs.readFileSync(path.join(root, 'cmd/9l/main.go'), 'utf8');
  const version = source.match(/^var version = "([^"]+)"$/m)?.[1];
  assert.equal(tag, `v${version}`);
  const ancestor = spawnSync('git', ['merge-base', '--is-ancestor', 'HEAD', 'origin/main'], {cwd: root, stdio: 'ignore'});
  assert.equal(ancestor.status, 0);
  if (process.env.GITHUB_OUTPUT) fs.appendFileSync(process.env.GITHUB_OUTPUT, `version=${version}\n`);
  console.log('CLI release tag, source version, public repository and main ancestry verified');
} catch {
  console.error('CLI release rejected: require matching v<major.minor.patch> tag on public main history');
  process.exitCode = 1;
}
