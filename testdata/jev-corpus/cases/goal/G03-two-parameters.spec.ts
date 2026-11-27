import {test, expect} from '@9l/playwright';

test("G03-two-parameters", async ({page, n9l}) => {
  await page.setContent(`<h1>Shipping</h1><label>City <input aria-label="City" id="city"></label><label>Postcode <input aria-label="Postcode" id="postcode"></label><button onclick="document.querySelector('[role=status]').textContent='Saved '+document.querySelector('#city').value+' '+document.querySelector('#postcode').value">Save address</button><p role="status">Unsaved</p>`);
  const result = await n9l.goal('Enter the city using parameter city and the postcode using parameter postcode, then save the address.', {params: {city: 'Lisbon', postcode: '1100-148'}});
  expect(result.verified).toBe(false);
  await expect(page.getByRole('status')).toHaveText('Saved Lisbon 1100-148');
});
