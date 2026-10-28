const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const crypto = require('node:crypto');
const {spawnSync} = require('node:child_process');
const {semver, declaredVersions, releaseVersion, releaseChannel} = require('./release-version.cjs');

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

test('only stable and beta releases select a publication channel', () => {
  for (const version of ['0.5.5', '1.0.0']) assert.equal(releaseChannel(version), 'latest');
  for (const version of ['0.6.0-beta', '0.6.0-beta.1', '1.0.0-beta.0']) assert.equal(releaseChannel(version), 'beta');
  for (const version of ['0.6.0-beta.01', '0.6.0-', '0.6.0-beta.', '0.6.0-rc.1', '00.6.0', 'v0.6.0', '0.6.0+build', '0.6.0\n']) {
    assert.equal(semver.test(version), false, version);
    assert.throws(() => releaseChannel(version), /unsupported/, version);
  }
});

// Exercise the actual release scripts in an isolated source tree. The preload
// replaces subprocesses, so these tests cannot publish or consult credentials.
function releaseFixture(version, run) {
  const copy = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'release-scripts-')));
  const write = (file, content) => {
    fs.mkdirSync(path.dirname(path.join(copy, file)), {recursive: true});
    fs.writeFileSync(path.join(copy, file), content);
  };
  try {
    for (const file of ['release-version.cjs', 'verify-release.cjs', 'publish-sdk.cjs', 'sdk-registry.cjs']) {
      write(`scripts/${file}`, fs.readFileSync(path.join(root, 'scripts', file)));
    }
    write('cmd/9l/main.go', `var version = "${version}"\n`);
    write('packages/playwright/package.json', JSON.stringify({
      name: '@9l/playwright', version, license: 'Apache-2.0',
      repository: {url: 'git+https://github.com/Quality-Max/9lives-runner.git'},
    }));
    write('package-lock.json', JSON.stringify({packages: {'packages/playwright': {version}}}));
    write('testdata/sdk/package.json', JSON.stringify({devDependencies: {'@9l/playwright': version}}));
    const artifact = Buffer.from('qualified test artifact');
    write(`.context/npm-pack/9l-playwright-${version}.tgz`, artifact);
    write('.context/npm-pack/qualification.json', JSON.stringify({
      name: '@9l/playwright', version, sha256: crypto.createHash('sha256').update(artifact).digest('hex'),
      results: [
        {spec: 'checkout', adapter: 'sdk', status: 'passed', validated: true, executedTests: 1},
        {spec: 'business-failure', adapter: 'sdk', status: 'failed', validated: true, executedTests: 1},
        {spec: 'ordinary', adapter: 'ordinary', status: 'passed', validated: true, executedTests: 1},
      ],
    }));
    write('subprocess.cjs', `const fs = require('node:fs');
require('node:child_process').spawnSync = (command, args) => {
  fs.appendFileSync(${JSON.stringify(path.join(copy, 'calls.jsonl'))}, JSON.stringify({command, args}) + '\\n');
  if (command === 'git') return {status: 0};
  if (command === 'npm' && args[0] === 'view') return {status: 1, stdout: ''};
  if (command === 'npm' && args[0] === 'publish') return {status: 0};
  throw new Error('unexpected subprocess');
};\n`);
    run({copy, write, execute: script => spawnSync(process.execPath, ['--require', path.join(copy, 'subprocess.cjs'), path.join(copy, 'scripts', script)], {
      encoding: 'utf8', timeout: 10000,
      env: {GITHUB_REF_TYPE: 'tag', GITHUB_REF_NAME: `v${version}`, GITHUB_REPOSITORY: 'Quality-Max/9lives-runner', REPOSITORY_PRIVATE: 'false', GITHUB_OUTPUT: path.join(copy, 'outputs')},
    })});
  } finally {
    fs.rmSync(copy, {recursive: true, force: true});
  }
}

for (const [version, channel] of [['0.5.5', 'latest'], ['0.6.0-beta', 'beta']]) {
  test(`${version} verifies its tag and publishes the qualified bytes on ${channel}`, () => {
    releaseFixture(version, ({copy, execute}) => {
      const verified = execute('verify-release.cjs');
      assert.equal(verified.status, 0, verified.stderr);
      assert.equal(fs.readFileSync(path.join(copy, 'outputs'), 'utf8'), `version=${version}\nprerelease=${channel === 'beta'}\n`);
      const published = execute('publish-sdk.cjs');
      assert.equal(published.status, 0, published.stderr);
      const calls = fs.readFileSync(path.join(copy, 'calls.jsonl'), 'utf8').trim().split('\n').map(JSON.parse);
      assert.deepEqual(calls.filter(c => c.command === 'npm' && c.args[0] === 'publish'), [{
        command: 'npm', args: ['publish', path.join(copy, '.context/npm-pack', `9l-playwright-${version}.tgz`), '--access', 'public', '--tag', channel],
      }]);
    });
  });
}

test('a beta artifact with altered bytes is rejected before publication', () => {
  releaseFixture('0.6.0-beta', ({copy, write, execute}) => {
    write('.context/npm-pack/9l-playwright-0.6.0-beta.tgz', 'altered artifact');
    assert.notEqual(execute('publish-sdk.cjs').status, 0);
    assert.equal(fs.existsSync(path.join(copy, 'calls.jsonl')), false);
  });
});
