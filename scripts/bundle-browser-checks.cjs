// Maintainer step after changing packages/browser-checks: npm run build, then
// node scripts/bundle-browser-checks.cjs. The 9l binary embeds the committed
// JavaScript, so plain Go builds need neither npm nor a TypeScript compiler.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');

const root = path.resolve(__dirname, '..');
const source = path.join(root, 'packages/browser-checks/dist');
const destination = path.join(root, 'internal/browsercheck/helper');

function scripts(dir, prefix = '') {
  if (!fs.existsSync(dir)) return [];
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const name = prefix ? `${prefix}/${entry.name}` : entry.name;
    if (entry.isDirectory()) return scripts(path.join(dir, entry.name), name);
    return name.endsWith('.js') ? [name] : [];
  }).sort();
}

const built = scripts(source);
assert.ok(built.includes('helper.js'), 'build packages/browser-checks first: npm run build');
if (process.argv.includes('--check')) {
  assert.deepEqual(scripts(destination), built, 'embedded browser checks differ: file list');
  for (const name of built) {
    assert.equal(fs.readFileSync(path.join(destination, name), 'utf8'), fs.readFileSync(path.join(source, name), 'utf8'),
      `embedded browser checks differ: ${name}; run node scripts/bundle-browser-checks.cjs`);
  }
  console.log(`Embedded browser checks match packages/browser-checks (${built.length} files).`);
} else {
  fs.rmSync(destination, { recursive: true, force: true });
  for (const name of built) {
    fs.mkdirSync(path.dirname(path.join(destination, name)), { recursive: true });
    fs.copyFileSync(path.join(source, name), path.join(destination, name));
  }
  console.log(`Embedded ${built.length} browser-check files in internal/browsercheck/helper.`);
}
