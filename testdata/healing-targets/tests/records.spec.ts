import { test, expect } from '@playwright/test';
import { installRecords, attachObservation, type Observation } from '../fixture';

let observation: Observation | undefined;
test.beforeEach(async ({ page }) => {
  observation = undefined;
  if (process.env.NINELIVES_TARGET_CONTROL === 'timeout') test.setTimeout(500);
  observation = await installRecords(page);
});
test.afterEach(async ({ page }, info) => {
  await attachObservation(page, info, observation);
});

// Intentionally inadequate: opening any editor does not protect record identity.
// @9l-requirement intended-record
test('weak check accepts any open editor', async ({ page }) => {
  const target = process.env.NINELIVES_TARGET_CONTROL === 'wrong-row' ? 'record-b' : 'record-a';
  await page.getByRole('row').filter({ has: page.getByRole('cell', { name: target, exact: true }) })
    .getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Record editor', exact: true })).toBeVisible();
});

// @9l-requirement intended-record
test('strong check requires the intended record ID', async ({ page }) => {
  const target = process.env.NINELIVES_TARGET_CONTROL === 'wrong-row' ? 'record-b' : 'record-a';
  await page.getByRole('row').filter({ has: page.getByRole('cell', { name: target, exact: true }) })
    .getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Record editor', exact: true })).toBeVisible();
  // @9l-outcome exact-record-id
  await expect(page.getByTestId('editor-record-id')).toHaveText('record-a');
});
