import {test, expect} from '@9l/playwright';
test('identical buttons in named forms are told apart by their form', async ({page, n9l}) => {
  await page.setContent(`<p role="status">Ready</p>
    <form aria-label="Shipping address"><button type="button" onclick="document.querySelector('[role=status]').textContent='Shipping saved'">Submit</button></form>
    <form aria-label="Billing address"><button type="button" onclick="document.querySelector('[role=status]').textContent='Billing saved'">Submit</button></form>`);
  await n9l.goal('Submit the shipping address form.');
  await expect(page.getByRole('status')).toHaveText('Shipping saved');
});
