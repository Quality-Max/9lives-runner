import { test, expect } from '@playwright/test';
import { installRecords, attachObservation, type Observation } from '../fixture';

let observation: Observation | undefined;
test.beforeEach(async ({ page }) => {
  observation = await installRecords(page);
});
test.afterEach(async ({ page }, info) => {
  await attachObservation(page, info, observation);
});

// Separate assertion canary: the action is correct; the expected ID is
// deliberately false. This must fail, never be treated as a baseline test.
test('wrong expected ID canary', async ({ page }) => {
  await page.getByRole('row').filter({ has: page.getByRole('cell', { name: 'record-a', exact: true }) })
    .getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.getByTestId('editor-record-id')).toHaveText('record-b');
});
