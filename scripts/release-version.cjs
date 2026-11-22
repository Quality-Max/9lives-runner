// The CLI and @9l/playwright share one version and are released together
// from one v<version> tag. These are every place that version is declared.
const fs = require('node:fs');
const path = require('node:path');

// Stable releases and explicit beta prereleases are the supported channels.
const semver = /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-beta(?:\.(?:0|[1-9]\d*))?)?$/;

function releaseChannel(version) {
  if (!semver.test(version)) throw new Error('unsupported release version');
  return version.includes('-') ? 'beta' : 'latest';
}

function compareVersions(left, right) {
  releaseChannel(left);
  releaseChannel(right);
  const parts = version => version.split(/[-.]/).map(part => part === 'beta' ? part : BigInt(part));
  const a = parts(left), b = parts(right);
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) return a[i] > b[i] ? 1 : -1;
  }
  if (a.length === 3 || b.length === 3) return Math.sign(b.length - a.length);
  const an = a[4] ?? -1n, bn = b[4] ?? -1n;
  return an === bn ? 0 : an > bn ? 1 : -1;
}

function declaredVersions(root = path.resolve(__dirname, '..')) {
  const source = fs.readFileSync(path.join(root, 'cmd/9l/main.go'), 'utf8');
  const read = file => JSON.parse(fs.readFileSync(path.join(root, file), 'utf8'));
  const lock = read('package-lock.json');
  return {
    cli: source.match(/^var version = "([^"]+)"$/m)?.[1],
    sdk: read('packages/playwright/package.json').version,
    lockfile: lock.packages?.['packages/playwright']?.version,
    fixture: read('testdata/sdk/package.json').devDependencies?.['@9l/playwright'],
  };
}

// The one release version, or an error naming the declarations that differ.
function releaseVersion(root) {
  const versions = declaredVersions(root);
  const distinct = new Set(Object.values(versions));
  if (distinct.size !== 1 || !semver.test(versions.cli || '')) {
    throw new Error(`release versions differ: ${Object.entries(versions).map(([where, version]) => `${where} ${version}`).join(', ')}`);
  }
  return versions.cli;
}

// The source files that declare the version, each with the one exact text
// that holds it. A file whose text is not found exactly once is not changed.
const declarations = [
  {file: 'cmd/9l/main.go', text: version => `var version = "${version}"`, anchored: true},
  {file: 'packages/playwright/package.json', text: version => `"version": "${version}"`},
  {file: 'testdata/sdk/package.json', text: version => `"@9l/playwright": "${version}"`},
];

// setReleaseVersion moves every declaration from the current release version
// to next, then refreshes the lockfile through npm, so the lockfile is never
// edited by hand. It refuses a version that is not supported, not later than
// the current one, or a tree whose declarations already differ. It checks
// every file before writing any, and checks the result.
function setReleaseVersion(next, {root = path.resolve(__dirname, '..'), refreshLockfile = defaultRefreshLockfile} = {}) {
  releaseChannel(next);
  const current = releaseVersion(root);
  if (compareVersions(next, current) <= 0) throw new Error(`${next} is not later than the current version ${current}`);
  const edits = declarations.map(({file, text, anchored}) => {
    const source = fs.readFileSync(path.join(root, file), 'utf8');
    const from = text(current);
    const count = anchored ? source.split('\n').filter(line => line === from).length : source.split(from).length - 1;
    if (count !== 1) throw new Error(`${file}: expected the version declaration ${from} exactly once, found ${count}`);
    return {file, content: source.replace(from, text(next))};
  });
  for (const {file, content} of edits) fs.writeFileSync(path.join(root, file), content);
  refreshLockfile(root);
  const result = releaseVersion(root);
  if (result !== next) throw new Error(`release version is ${result} after the update, not ${next}`);
  return {from: current, to: next, files: [...declarations.map(({file}) => file), 'package-lock.json']};
}

function defaultRefreshLockfile(root) {
  const {spawnSync} = require('node:child_process');
  const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm';
  const run = spawnSync(npm, ['install', '--package-lock-only', '--ignore-scripts', '--no-audit', '--no-fund'], {cwd: root, stdio: 'inherit', shell: process.platform === 'win32'});
  if (run.status !== 0) throw new Error('npm install --package-lock-only failed; the version declarations were already updated');
}

module.exports = {semver, declaredVersions, releaseVersion, releaseChannel, compareVersions, setReleaseVersion};

// node scripts/release-version.cjs          prints the release version
// node scripts/release-version.cjs --set V  sets every declaration to V
if (require.main === module) {
  const args = process.argv.slice(2);
  try {
    if (args.length === 0) {
      console.log(releaseVersion());
    } else if (args.length === 2 && args[0] === '--set') {
      const {from, to, files} = setReleaseVersion(args[1]);
      console.log(`Release version ${from} -> ${to} in ${files.join(', ')}.`);
      console.log('Still by hand: the CHANGELOG.md section and summary, and for a beta the install example in docs/releases.md.');
    } else {
      console.error('usage: node scripts/release-version.cjs [--set <version>]');
      process.exit(2);
    }
  } catch (error) {
    console.error(`release-version: ${error.message}`);
    process.exit(1);
  }
}
