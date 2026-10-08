const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const zlib = require('node:zlib');
const {spawnSync} = require('node:child_process');

test('parser qualification accepts alternate gzip encoding and rejects changed source, license and manifest', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), '9l-parser-check-'));
  try {
    const packageDir = path.join(root, 'node_modules/typescript');
    const destination = path.join(root, 'internal/assessment/parser');
    fs.mkdirSync(path.join(packageDir, 'lib'), {recursive: true});
    fs.mkdirSync(destination, {recursive: true});
    fs.mkdirSync(path.join(root, 'scripts'));
    fs.copyFileSync(path.join(__dirname, 'bundle-assessment-parser.cjs'), path.join(root, 'scripts/check.cjs'));
    fs.writeFileSync(path.join(packageDir, 'package.json'), JSON.stringify({version: '5.9.3'}));
    const parser = Buffer.from('/* controlled parser qualification fixture */\n'.repeat(100));
    const license = Buffer.from('Controlled license fixture\r\n');
    fs.writeFileSync(path.join(packageDir, 'lib/typescript.js'), parser);
    fs.writeFileSync(path.join(packageDir, 'LICENSE.txt'), license);
    const compressed = zlib.gzipSync(parser, {level: 1});
    assert.notDeepEqual(compressed, zlib.gzipSync(parser, {level: 9}));
    const hash = data => crypto.createHash('sha256').update(data).digest('hex');
    const manifest = {version: '5.9.3', sourceSHA256: hash(parser), compressedSHA256: hash(compressed), licenseSHA256: hash(license)};
    const reset = () => {
      fs.writeFileSync(path.join(destination, 'typescript.js.gz'), compressed);
      fs.writeFileSync(path.join(destination, 'LICENSE.txt'), license);
      fs.writeFileSync(path.join(destination, 'manifest.json'), JSON.stringify(manifest));
    };
    const check = () => spawnSync(process.execPath, [path.join(root, 'scripts/check.cjs'), '--check'],
      {env: {}, encoding: 'utf8', timeout: 10000}).status;
    reset();
    assert.equal(check(), 0, 'same source in a different gzip stream must qualify');
    const foreign = zlib.gzipSync(Buffer.from('changed source'));
    fs.writeFileSync(path.join(destination, 'typescript.js.gz'), foreign);
    fs.writeFileSync(path.join(destination, 'manifest.json'), JSON.stringify({...manifest, compressedSHA256: hash(foreign)}));
    assert.notEqual(check(), 0, 'a matching compressed checksum must not admit changed source');
    reset();
    fs.writeFileSync(path.join(destination, 'LICENSE.txt'), 'changed license');
    assert.notEqual(check(), 0);
    reset();
    fs.writeFileSync(path.join(destination, 'manifest.json'), JSON.stringify({...manifest, compressedSHA256: '0'.repeat(64)}));
    assert.notEqual(check(), 0);
  } finally {
    fs.rmSync(root, {recursive: true, force: true});
  }
});
