const assert = require('node:assert/strict');
const test = require('node:test');

const { redactSensitiveText, redactSensitiveData } = require('../dist/redact.js');
const { looksStateChanging } = require('../dist/scan-url.js');

test('common credential shapes are redacted', () => {
  const jwt = 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJl';
  const cases = [
    ['https://user:pass@example.com/a', 'https://[REDACTED]@example.com/a'],
    ['/cb?code=1&id_token=abc.def&state=x', '/cb?code=1&id_token=[REDACTED]&state=x'],
    ['/a?refresh_token=r1&x-api-token=t2', '/a?refresh_token=[REDACTED]&x-api-token=[REDACTED]'],
    ['session_id=s3cr3t; path=/', 'session_id=[REDACTED] path=/'],
    ['Authorization: Basic dXNlcjpwYXNz', 'Authorization: Basic [REDACTED]'],
    ['Authorization: Bearer abc.def', 'Authorization: Bearer [REDACTED]'],
    [`token in page: ${jwt}`, 'token in page: [REDACTED]'],
  ];
  for (const [input, expected] of cases) assert.equal(redactSensitiveText(input), expected, input);
  assert.deepEqual(redactSensitiveData({ list: [`jwt ${jwt}`], n: 1 }), { list: ['jwt [REDACTED]'], n: 1 });
});

test('ordinary text is left alone', () => {
  for (const text of ['Sign in to continue', 'Tokens of appreciation', 'https://example.com/docs/session-replay']) {
    assert.equal(redactSensitiveText(text), text);
  }
});

test('links that sign out or change data are recognised', () => {
  for (const href of ['/logout', '/users/sign_out', '/account/log-off', '/auth/signout?next=/', '/items/7/delete', '/api?action=remove&id=3', '/newsletter/unsubscribe', '/%6Cogout']) {
    assert.equal(looksStateChanging(new URL(href, 'https://example.com')), true, href);
  }
  for (const href of ['/', '/docs/logging', '/blog/deleted-scenes-review', '/settings', '/catalog?q=removals']) {
    assert.equal(looksStateChanging(new URL(href, 'https://example.com')), false, href);
  }
});
