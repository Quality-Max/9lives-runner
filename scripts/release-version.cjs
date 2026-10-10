// The CLI and @9l/playwright share one version and are released together
// from one v<version> tag. These are every place that version is declared.
const fs = require('node:fs');
const path = require('node:path');

const semver = /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/;

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

module.exports = {semver, declaredVersions, releaseVersion};
