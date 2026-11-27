import {test, expect} from '@9l/playwright';

test("G06-wrong-business-outcome", async ({page, n9l}) => {
  await page.setContent(`<label>Name <input aria-label="Name"></label><button onclick="document.querySelector('[role=status]').textContent='Declined'">Place booking</button><p role="status">Ready</p>`);
  const result = await n9l.goal('Fill Name using parameter name, then place the booking.', {params: {name: 'Fixture Person'}});
  expect(result.verified).toBe(false);
  await expect(page.getByRole('status')).toHaveText('Booking confirmed');
});
