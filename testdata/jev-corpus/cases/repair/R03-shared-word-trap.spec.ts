import {test, expect} from '@playwright/test';

test("R03-shared-word-trap", async ({page}) => {
  await page.setContent(`<h1>Article editor</h1><textarea>Hello</textarea><button onclick="document.body.insertAdjacentHTML('beforeend','<p role=status>Published</p>')">Save and publish</button><button onclick="document.body.insertAdjacentHTML('beforeend','<p role=status>Draft deleted</p>')">Discard draft</button><button onclick="document.body.insertAdjacentHTML('beforeend','<p role=status>Draft saved</p>')">Save as draft</button>`);
  await page.getByRole('button', { name: 'Save draft' }).click();
  await expect(page.getByRole('status')).toHaveText('Draft saved');
});
