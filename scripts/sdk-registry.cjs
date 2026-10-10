// Registry visibility for an SDK version. npm serves a version a minute or
// more after `npm publish` returns (0.1.2 and 0.1.3 appeared 1 to 2.5
// minutes later), so one lookup right after publication can miss it. A run
// that took that miss as "unpublished" re-qualified the release and failed
// trying to publish it twice.
const {spawnSync} = require('node:child_process');

const name = '@9l/playwright';
const semver = /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/;

// Exactly this version is on the registry. A lookup that fails for any
// reason, including the E404 npm reports for a missing version, is a miss.
function registryHas(version, run = spawnSync) {
  const view = run('npm', ['view', `${name}@${version}`, 'version'], {encoding: 'utf8', timeout: 60000});
  return !view.error && view.status === 0 && String(view.stdout).trim() === version;
}

// Look up to `attempts` times, `intervalMs` apart; true as soon as it is seen.
async function awaitVersion(version, {attempts, intervalMs, has = registryHas, sleep = ms => new Promise(resolve => setTimeout(resolve, ms))}) {
  for (let attempt = 1; ; attempt++) {
    if (has(version)) return true;
    if (attempt >= attempts) return false;
    await sleep(intervalMs);
  }
}

module.exports = {name, semver, registryHas, awaitVersion};

// `node scripts/sdk-registry.cjs await <version>` waits up to ten minutes for
// a just-published version, so its release tag is pushed only once a run
// started by that tag can see the version and skip it.
if (require.main === module) {
  const [command, version] = process.argv.slice(2);
  if (command !== 'await' || !semver.test(version || '')) {
    console.error('usage: node scripts/sdk-registry.cjs await <version>');
    process.exit(2);
  }
  awaitVersion(version, {attempts: 40, intervalMs: 15000}).then(seen => {
    if (seen) {
      console.log(`${name}@${version} is visible on the registry`);
    } else {
      console.error(`${name}@${version} is not visible on the registry after ten minutes; push the tag by hand once it is`);
      process.exitCode = 1;
    }
  });
}
