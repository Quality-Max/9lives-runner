/**
 * Two first-run failures: Playwright's missing-browser banner instead of the
 * install command, and a loopback refusal that read the same with and without
 * consent.
 */
const assert = require('node:assert/strict');
const test = require('node:test');

const { assertSafeNetworkUrl } = require('../dist/network-policy.js');
const { missingBrowserError } = require('../dist/common.js');

const resolve = async (hostname) => {
  const addresses = {
    hub: [{ address: '172.18.0.3', family: 4 }],
    'loopback-alias.example': [{ address: '127.0.0.1', family: 4 }],
    localhost: [{ address: '127.0.0.1', family: 4 }],
  };
  if (!addresses[hostname]) throw new Error('ENOTFOUND');
  return addresses[hostname];
};

async function refusal(url, options) {
  try {
    await assertSafeNetworkUrl(url, { lookup: resolve, ...options });
  } catch (error) {
    return error.message;
  }
  assert.fail(`${url} was permitted`);
}

test('with consent, a compose service name is refused with the rule and the workaround', async () => {
  const message = await refusal('http://hub:3000/login', { allowPrivateNetwork: true });
  assert.match(message, /not permitted by the local safety policy/);
  assert.match(message, /only to localhost, 127\.0\.0\.0\/8 and ::1/);
  assert.match(message, /Docker Compose service name/);
  assert.match(message, /http:\/\/localhost:<port>/);
  assert.doesNotMatch(message, /172\.18/, 'the resolved address is never disclosed');
});

test('with consent, a private answer and a failed lookup read the same', async () => {
  // Otherwise the message is an oracle for which internal names resolve.
  const privateAnswer = await refusal('http://hub:3000/', { allowPrivateNetwork: true });
  const noAnswer = await refusal('http://does-not-resolve:3000/', { allowPrivateNetwork: true });
  const loopbackAlias = await refusal('http://loopback-alias.example:3000/', { allowPrivateNetwork: true });
  assert.equal(privateAnswer, noAnswer);
  assert.equal(privateAnswer, loopbackAlias);
});

test('without consent, only an explicit loopback target is told about the opt-in', async () => {
  assert.match(await refusal('http://localhost:3000/', {}), /allow_private_network: true \(CLI: --allow-private-network\)/);
  assert.match(await refusal('http://127.0.0.1:3000/', {}), /allow_private_network: true/);
  // A name that merely resolves privately keeps the bare refusal: suggesting
  // the opt-in would confirm what it resolved to.
  assert.equal(await refusal('http://hub:3000/', {}), 'The requested network destination is not permitted by the local safety policy.');
});

test('with consent, a second loopback origin is refused by naming the approved one', async () => {
  const message = await refusal('http://localhost:4000/', {
    allowPrivateNetwork: true,
    privateNetworkOrigin: 'http://localhost:3000',
  });
  assert.match(message, /only the loopback origin it was given, http:\/\/localhost:3000/);
});

test('the missing-browser error names the project install command', () => {
  const banner = new Error(
    "browserType.launch: Executable doesn't exist at /x/chromium_headless_shell-1248/chrome-headless-shell\n║     npx playwright install     ║"
  );
  const error = missingBrowserError(banner, 'chromium');
  assert.match(error.message, /Install the matching build with: npx --yes playwright@[0-9.]+ install chromium\./);
  assert.doesNotMatch(error.message, /╔|║/, "Playwright's banner is replaced, not appended");
  assert.equal(missingBrowserError(new Error('net::ERR_CONNECTION_REFUSED'), 'chromium'), undefined);
});
