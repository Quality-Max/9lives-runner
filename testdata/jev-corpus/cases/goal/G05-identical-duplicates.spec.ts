import {test, expect} from '@9l/playwright';

test("G05-identical-duplicates", async ({page, n9l}) => {
  await page.setContent(`<h1>Survey</h1><form><button type="button" onclick="document.body.dataset.sent=1">Next</button></form><form><button type="button" onclick="document.body.dataset.sent=2">Next</button></form>`);
  await expect(n9l.goal('Go to the next step.')).rejects.toThrow();
  expect(await page.locator('body').getAttribute('data-sent')).toBeNull();
});
