import {test, expect} from '@9l/playwright';

test("G01-signup", async ({page, n9l}) => {
  await page.setContent(`<h1>Create account</h1><label>Email <input aria-label="Email"></label><button onclick="document.querySelector('[role=status]').textContent=document.querySelector('input').value?'Account created':'Email required'">Create account</button><p role="status">Not started</p>`);
  const result = await n9l.goal('Enter the email using parameter email, then create the account.', {params: {email: 'person@example.test'}});
  expect(result.verified).toBe(false);
  await expect(page.getByRole('status')).toHaveText('Account created');
});
