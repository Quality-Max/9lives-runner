// Whether an SDK version is already on the registry, so a rerun of a release
// never tries to publish the same version twice.
const {spawnSync} = require('node:child_process');

const name = '@9l/playwright';

// Exactly this version is on the registry. A lookup that fails for any
// reason, including the E404 npm reports for a missing version, is a miss.
function registryHas(version, run = spawnSync) {
  const view = run('npm', ['view', `${name}@${version}`, 'version'], {encoding: 'utf8', timeout: 60000});
  return !view.error && view.status === 0 && String(view.stdout).trim() === version;
}

module.exports = {name, registryHas};
