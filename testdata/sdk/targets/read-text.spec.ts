import {createTargetTest} from '@9l/playwright';

// text() returns what a user reads: a field's current value rather than its
// default, a select's chosen option, and visible text without scripts or
// hidden children. A hidden control is not readable, a password is refused,
// and over 4 KiB is refused.
const test = createTargetTest({web: {
  open: async page => {
    await page.setContent(`<textarea data-testid="notes">default</textarea>
      <input data-testid="code" value="CODE123" readonly>
      <select data-testid="plan"><option>basic</option><option selected>pro</option></select>
      <p data-testid="banner">Activation <b>code</b><span style="display:none"> hidden</span><script>var leak = 1;</script></p>
      <p data-testid="gone" style="display:none">not shown</p>
      <input data-testid="secret" type="password" value="hunter2">
      <p data-testid="long">${'x'.repeat(5000)}</p>`);
  },
  reset: async () => {},
}});

test('text reads what a user sees', async ({app}) => {
  await app.open();
  await app.getByTestId('notes').fill('typed');
  const read = [await app.getByTestId('notes').text(), await app.getByTestId('code').text(),
    await app.getByTestId('plan').text(), await app.getByTestId('banner').text()];
  if (JSON.stringify(read) !== JSON.stringify(['typed', 'CODE123', 'pro', 'Activation code'])) throw new Error('read ' + read.length + ' values that differ');
  await app.getByTestId('notes').toBeVisible();
});

test('text of a hidden control', async ({app}) => {
  await app.open();
  await app.getByTestId('gone').text({timeoutMs: 1000});
});

test('text of a password field', async ({app}) => {
  await app.open();
  await app.getByTestId('secret').text();
});

test('text over the bound', async ({app}) => {
  await app.open();
  await app.getByTestId('long').text();
});
