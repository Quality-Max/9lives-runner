// Maintainer-only: npm ci, then node scripts/bundle-assessment-parser.cjs.
// Runtime and plain Go builds use the committed asset without npm or downloads.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const zlib = require('node:zlib');
const assert = require('node:assert/strict');

const root = path.resolve(__dirname, '..');
const packageDir = path.join(root, 'node_modules/typescript');
const version = JSON.parse(fs.readFileSync(path.join(packageDir, 'package.json'))).version;
assert.equal(version, '5.9.3', 'qualify a new parser version before updating this pin');
const parser = fs.readFileSync(path.join(packageDir, 'lib/typescript.js'));
const license = fs.readFileSync(path.join(packageDir, 'LICENSE.txt'));
const compressed = zlib.gzipSync(parser, { level: 9 });
const hash = data => crypto.createHash('sha256').update(data).digest('hex');
const assets = {
  'typescript.js.gz': compressed,
  'LICENSE.txt': license,
  'manifest.json': Buffer.from(JSON.stringify({ version, sourceSHA256: hash(parser), compressedSHA256: hash(compressed), licenseSHA256: hash(license) }, null, 2) + '\n'),
};
const destination = path.join(root, 'internal/assessment/parser');
if (process.argv.includes('--check')) {
  // Different Node/zlib releases can produce different valid gzip streams.
  // Qualify the committed stream's contents and integrity, not its encoding.
  const committed = fs.readFileSync(path.join(destination, 'typescript.js.gz'));
  assert.deepEqual(zlib.gunzipSync(committed), parser, 'parser asset differs: decompressed source');
  assert.deepEqual(fs.readFileSync(path.join(destination, 'LICENSE.txt')), license, 'parser asset differs: LICENSE.txt');
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(destination, 'manifest.json'), 'utf8')),
    { version, sourceSHA256: hash(parser), compressedSHA256: hash(committed), licenseSHA256: hash(license) },
    'parser asset differs: manifest.json');
  console.log('Bundled TypeScript 5.9.3 matches the locked npm source and license.');
} else {
  fs.mkdirSync(destination, { recursive: true });
  for (const [name, bytes] of Object.entries(assets)) fs.writeFileSync(path.join(destination, name), bytes);
  console.log('Bundled TypeScript 5.9.3 with checksum manifest and Apache license.');
}
