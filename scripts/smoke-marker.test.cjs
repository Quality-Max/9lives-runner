const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {spawn} = require('node:child_process');
const test = require('node:test');

test('smoke readiness is invisible until the entire PID record is written', {timeout: 15000}, async () => {
  const work = fs.mkdtempSync(path.join(os.tmpdir(), '9l-marker-test-'));
  const marker = path.join(work, 'ready');
  const signal = path.join(work, 'opened');
  const release = path.join(work, 'release');
  const preload = path.join(work, 'pause-write.cjs');
  fs.writeFileSync(preload, `
    const fs = require('node:fs');
    const write = fs.writeFileSync;
    fs.writeFileSync = (file, data, ...rest) => {
      if (typeof file !== 'string' || !file.startsWith(process.env.NINELIVES_SMOKE_MARKER))
        return write(file, data, ...rest);
      const fd = fs.openSync(file, 'w');
      try {
        write(process.env.MARKER_TEST_SIGNAL, 'opened');
        const deadline = Date.now() + 10000;
        const pause = new Int32Array(new SharedArrayBuffer(4));
        while (!fs.existsSync(process.env.MARKER_TEST_RELEASE)) {
          if (Date.now() > deadline) throw new Error('marker test release budget exhausted');
          Atomics.wait(pause, 0, 0, 10);
        }
        fs.writeSync(fd, data);
      } finally { fs.closeSync(fd); }
    };
  `);
  const env = {
    NINELIVES_SMOKE_MARKER: marker, NINELIVES_SMOKE_PORT: '0',
    MARKER_TEST_SIGNAL: signal, MARKER_TEST_RELEASE: release,
  };
  if (process.env.SystemRoot) env.SystemRoot = process.env.SystemRoot;
  const child = spawn(process.execPath, ['--require', preload, path.resolve(__dirname, '../testdata/sdk/tests/webserver-cleanup/server.cjs')], {env, stdio: 'ignore'});
  const closed = new Promise((resolve, reject) => {
    child.once('error', reject);
    child.once('close', resolve);
  });
  const waitFor = async file => {
    const deadline = Date.now() + 5000;
    while (!fs.existsSync(file)) {
      assert.equal(child.exitCode, null, 'marker producer exited before publishing');
      assert(Date.now() < deadline, 'marker producer did not reach the expected state');
      await new Promise(resolve => setTimeout(resolve, 10));
    }
  };
  try {
    await waitFor(signal);
    assert.equal(fs.existsSync(`${marker}.server`), false, 'an unfinished write announced readiness');
    fs.writeFileSync(release, 'continue');
    await waitFor(`${marker}.server`);
    assert.deepEqual(JSON.parse(fs.readFileSync(`${marker}.server`, 'utf8')), {serverPID: child.pid});
  } finally {
    child.kill('SIGTERM');
    await closed;
    fs.rmSync(work, {recursive: true, force: true});
  }
});
