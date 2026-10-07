import {test, expect} from '@9lives/playwright';

test('synthetic checkout confirms an order', async ({page, nineLives}, testInfo) => {
  await page.setContent(`<button type="button" onclick="document.querySelector('[role=status]').textContent='Order confirmed: 1 item'">Place order</button><p role="status">Cart: 1 item</p>`);
  await nineLives.step('place the order', async () => {
    await page.getByRole('button', {name: 'Place order'}).click();
  });
  await nineLives.step('verify the checkout outcome', async () => {
    await expect(page.getByRole('status')).toHaveText('Order confirmed: 1 item');
  });
  await testInfo.attach('synthetic outcome', {body: Buffer.from('{"confirmed":true}'), contentType: 'application/json'});
  console.log('synthetic worker log must stay outside the engine protocol');
});
