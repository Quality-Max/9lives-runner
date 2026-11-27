'use strict';
// 9l provenance records the branch and deliberately refuses a detached HEAD,
// such as a release tag checkout. Smokes call this before any slow setup, so
// the requirement is named up front instead of failing later as `2 !== 0`.
const {spawnSync} = require('node:child_process');

function detachedHead(root) {
  const result = spawnSync('git', ['symbolic-ref', '-q', 'HEAD'], {cwd: root, encoding: 'utf8', timeout: 10000});
  if (result.error) throw result.error;
  return result.status !== 0;
}

function requireBranch(root, smoke) {
  if (detachedHead(root)) {
    throw new Error(`${smoke} needs a branch: this checkout is a detached HEAD (e.g. a release tag). ` +
      'Run `git switch -c <name>` at the same commit first.');
  }
}

// 9l provenance prints only a fixed error line, never source or Git output.
const provenanceFailure = result => `9l provenance: ${String(result.stderr || '').trim().split('\n')[0].slice(0, 200)}`;

module.exports = {detachedHead, requireBranch, provenanceFailure};
