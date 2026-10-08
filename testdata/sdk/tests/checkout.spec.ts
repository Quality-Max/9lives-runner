import {test, expect} from '@9l/playwright';

test('synthetic checkout confirms an order', async ({page, n9l}, testInfo) => {
  await page.setContent(`<button type="button" onclick="document.querySelector('[role=status]').textContent='Order confirmed: 1 item'">Place order</button><p role="status">Cart: 1 item</p>`);
  await n9l.step('place the order', async () => {
    await page.getByRole('button', {name: 'Place order'}).click();
  });
  await n9l.step('verify the checkout outcome', async () => {
    await expect(page.getByRole('status')).toHaveText('Order confirmed: 1 item');
  });
  await testInfo.attach('synthetic outcome', {body: Buffer.from('{"confirmed":true}'), contentType: 'application/json'});
  console.log('synthetic worker log must stay outside the engine protocol');
});
