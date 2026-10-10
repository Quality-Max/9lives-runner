// Qualify `9l confirm` with real Chromium. A throwaway Git repository holds
// an app with a buggy commit, a fixing commit and an unrelated commit; an
// untracked reproduction spec asserts the fixed behavior. Confirm must say
// confirmed across the fix, not-reproduced across the unrelated change, and
// confirmed again with the fix uncommitted in the working tree. No account,
// provider or external server is involved.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {spawnSync} = require('node:child_process');

const root = path.resolve(__dirname, '..');
const work = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), '9lives-confirm-smoke-')));
const engine = path.join(work, process.platform === 'win32' ? '9l.exe' : '9l');
const repo = path.join(work, 'repo');
const receipts = path.join(work, 'receipts');
let stage = 'engine-build';

function invoke(command, args, cwd = root) {
  const env = {};
  for (const key of ['PATH', 'HOME', 'TMPDIR', 'TEMP', 'TMP', 'SystemRoot', 'USERPROFILE', 'LOCALAPPDATA', 'APPDATA', 'PLAYWRIGHT_BROWSERS_PATH']) {
    if (process.env[key] !== undefined) env[key] = process.env[key];
  }
  const result = spawnSync(command, args, {cwd, env, encoding: 'utf8', timeout: 300000, maxBuffer: 4 << 20});
  assert(!result.error, 'owned invocation failed or timed out');
  return result;
}

function git(...args) {
  const result = invoke('git', ['-C', repo, '-c', 'user.name=9lives', '-c', 'user.email=9lives@example.invalid', '-c', 'commit.gpgsign=false', ...args]);
  assert.equal(result.status, 0, `git ${args[0]} failed`);
  return result.stdout.trim();
}

function write(relative, content) {
  const file = path.join(repo, relative);
  fs.mkdirSync(path.dirname(file), {recursive: true});
  fs.writeFileSync(file, content);
}

const buggy = 'export const count = (items: string[]) => items.length - 1;\n';
const fixedApp = 'export const count = (items: string[]) => items.length;\n';

function confirm(...args) {
  const result = invoke(engine, ['confirm', 'tests/repro.spec.ts', '--format', 'json', '--timeout', '60s', '--receipt-dir', receipts, ...args], repo);
  return {status: result.status, report: result.stdout ? JSON.parse(result.stdout) : undefined};
}

function receiptsValidated(report) {
  for (const runId of [report.unfixed.runId, report.fixed.runId]) {
    const result = invoke(engine, ['result', runId, '--receipt-dir', receipts]);
    assert.equal(result.status === 0 || result.status === 1, true);
    const [receipt] = JSON.parse(result.stdout).receipts;
    assert.equal(receipt.validated, true);
    assert.equal(receipt.executedTests, 1);
  }
}

async function main() {
  assert.equal(invoke('go', ['build', '-o', engine, './cmd/9l']).status, 0, 'engine build failed');
  stage = 'repository';
  const version = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8')).devDependencies['@playwright/test'];
  write('package.json', JSON.stringify({private: true, devDependencies: {'@playwright/test': version}}) + '\n');
  write('.gitignore', 'node_modules/\n');
  write('app/shop.ts', buggy);
  git('init', '--quiet');
  git('add', '.');
  git('commit', '--quiet', '-m', 'buggy');
  const c1 = git('rev-parse', 'HEAD');
  write('app/shop.ts', fixedApp);
  git('commit', '--quiet', '-am', 'fix');
  const c2 = git('rev-parse', 'HEAD');
  write('README.md', 'Unrelated change.\n');
  git('add', 'README.md');
  git('commit', '--quiet', '-m', 'docs');
  const c3 = git('rev-parse', 'HEAD');
  // The workspace's installed dependencies, including the built SDK.
  fs.symlinkSync(path.join(root, 'node_modules'), path.join(repo, 'node_modules'), 'junction');
  write('tests/repro.spec.ts', `import {test, expect} from '@9l/playwright';
import {count} from '../app/shop';

test('an order of two items confirms two items', async ({page}) => {
  await page.setContent('<p role="status">Order confirmed: ' + count(['Book', 'Pen']) + ' items</p>');
  await expect(page.getByRole('status')).toHaveText('Order confirmed: 2 items', {timeout: 2000});
});
`);

  stage = 'confirm across the fix';
  const fix = confirm('--unfixed', c1, '--fixed', c2, '--finding-id', 'smoke-1');
  assert.equal(fix.status, 0);
  assert.equal(fix.report.verdict, 'confirmed');
  assert.equal(fix.report.policy, 'confirm-v1');
  assert.equal(fix.report.unfixed.status, 'failed');
  assert.equal(fix.report.fixed.status, 'passed');
  assert.equal(fix.report.unfixed.spec, 'absent');
  assert.deepEqual(fix.report.tests.map(test => `${test.unfixed}/${test.fixed}:${test.result}`), ['assertion-failed/passed:confirmed']);
  receiptsValidated(fix.report);

  stage = 'unrelated change';
  const unrelated = confirm('--unfixed', c2, '--fixed', c3);
  assert.equal(unrelated.status, 1);
  assert.equal(unrelated.report.verdict, 'not-reproduced');
  receiptsValidated(unrelated.report);

  stage = 'fix in the working tree';
  git('checkout', '--quiet', c1);
  write('app/shop.ts', fixedApp);
  const uncommitted = confirm('--unfixed', 'HEAD');
  assert.equal(uncommitted.status, 0);
  assert.equal(uncommitted.report.verdict, 'confirmed');
  assert.equal(uncommitted.report.fixed.ref, 'working-tree');
  assert.equal(uncommitted.report.fixed.dirty, true);
  receiptsValidated(uncommitted.report);

  stage = 'cleanup';
  assert.equal((git('worktree', 'list', '--porcelain').match(/^worktree /gm) || []).length, 1, 'a revision checkout was left registered');
  assert(fs.existsSync(path.join(root, 'node_modules', '@playwright', 'test', 'package.json')), 'cleanup reached the installed dependencies');
  const saved = fs.readdirSync(path.join(receipts, 'confirmations'));
  assert.equal(saved.length, 3);
  console.log('Confirm: real Chromium; confirmed across the fix (assertion failed, then passed), not-reproduced across an unrelated commit, confirmed with the fix uncommitted; 6 validated receipts, no checkout left behind');
}

main().catch(error => {
  const kind = error instanceof assert.AssertionError ? 'assertion' : error instanceof SyntaxError ? 'invalid-json' : 'execution';
  console.error(`Confirm smoke failed: ${stage} (${kind})`);
  if (process.env.NINELIVES_SMOKE_DEBUG) console.error(error);
  process.exitCode = 1;
}).finally(() => {
  // Remove the dependency link first, so the removal below never follows it.
  try { fs.unlinkSync(path.join(repo, 'node_modules')); } catch {}
  fs.rmSync(work, {recursive: true, force: true});
});
