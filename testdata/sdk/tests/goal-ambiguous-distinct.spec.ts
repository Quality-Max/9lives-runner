import {test, expect} from '@9l/playwright';
test('identical buttons in explicitly named landmarks are told apart', async ({page, n9l}) => {
  await page.setContent(`<p role="status">Ready</p>
    <div role="main" aria-label="Shipping address"><section><button type="button" onclick="document.querySelector('[role=status]').textContent='Shipping saved'">Submit</button></section></div>
    <div role="complementary" aria-label="Billing address"><section><button type="button" onclick="document.querySelector('[role=status]').textContent='Billing saved'">Submit</button></section></div>`);
  await n9l.goal('Submit the shipping address form.');
  await expect(page.getByRole('status')).toHaveText('Shipping saved');
});
