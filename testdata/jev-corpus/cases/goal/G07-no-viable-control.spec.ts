import {test, expect} from '@9l/playwright';

test("G07-no-viable-control", async ({page, n9l}) => {
  await page.setContent(`<h1>Order 1042</h1><p>Shipped on 3 November.</p><button>Back</button>`);
  await expect(n9l.goal('Download the invoice PDF.')).rejects.toThrow();
});
