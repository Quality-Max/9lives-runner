const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {spawnSync} = require('node:child_process');
const {detachedHead, requireBranch, provenanceFailure} = require('./smoke-branch.cjs');

test('smokes name the branch requirement on a detached HEAD', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-smoke-branch-'));
  const git = (...args) => assert.equal(spawnSync('git', ['-c', 'user.email=smoke@example.invalid', '-c', 'user.name=smoke', ...args], {cwd: dir}).status, 0, args.join(' '));
  try {
    git('init', '-q'); git('commit', '-q', '--allow-empty', '-m', 'fixture');
    assert.equal(detachedHead(dir), false);
    assert.doesNotThrow(() => requireBranch(dir, 'smoke:fixture'));
    git('checkout', '-q', '--detach');
    assert.equal(detachedHead(dir), true);
    assert.throws(() => requireBranch(dir, 'smoke:fixture'), /smoke:fixture needs a branch.*git switch -c <name>/);
  } finally { fs.rmSync(dir, {recursive: true, force: true}); }
});

test('a provenance failure reports one bounded line', () => {
  assert.equal(provenanceFailure({stderr: '9l: invalid agent provenance or detached branch\nmore\n'}), '9l provenance: 9l: invalid agent provenance or detached branch');
  assert.equal(provenanceFailure({stderr: 'x'.repeat(500)}).length, '9l provenance: '.length + 200);
  assert.equal(provenanceFailure({}), '9l provenance: ');
});

// CI checks a pull request out as a detached HEAD. Listing the Android cases
// must work there; only a run, which records provenance here, needs a branch.
test('android-smoke --list works on a detached HEAD and a run names the branch requirement', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-smoke-list-'));
  const git = (...args) => assert.equal(spawnSync('git', ['-c', 'user.email=smoke@example.invalid', '-c', 'user.name=smoke', ...args], {cwd: dir}).status, 0, args.join(' '));
  try {
    fs.mkdirSync(path.join(dir, 'scripts'));
    for (const file of ['android-smoke.cjs', 'android-isolation-evidence.cjs', 'smoke-branch.cjs']) {
      fs.copyFileSync(path.join(__dirname, file), path.join(dir, 'scripts', file));
    }
    git('init', '-q'); git('add', '.'); git('commit', '-q', '-m', 'fixture'); git('checkout', '-q', '--detach');
    const list = spawnSync(process.execPath, [path.join(dir, 'scripts', 'android-smoke.cjs'), '--list'], {cwd: dir, encoding: 'utf8'});
    assert.equal(list.status, 0, list.stderr);
    assert.ok(list.stdout.split('\n').includes('healthy'));
    const run = spawnSync(process.execPath, [path.join(dir, 'scripts', 'android-smoke.cjs'), 'healthy'], {cwd: dir, encoding: 'utf8'});
    assert.notEqual(run.status, 0);
    assert.match(run.stderr, /smoke:android needs a branch/);
  } finally { fs.rmSync(dir, {recursive: true, force: true}); }
});
