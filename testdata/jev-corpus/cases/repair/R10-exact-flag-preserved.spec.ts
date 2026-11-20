import {test, expect} from '@playwright/test';

test("R10-exact-flag-preserved", async ({page}) => {
  await page.setContent(`<h1>Job application</h1><button>Back</button><button>Cancel</button><button onclick="document.body.insertAdjacentHTML('beforeend','<p role=status>Application sent</p>')">Send application</button>`);
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Application sent');
});
