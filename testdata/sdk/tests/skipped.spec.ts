import {test, expect} from '@9lives/playwright';

test('ordinary assertion passes', async () => {
  expect(1 + 1).toBe(2);
});

test.skip('selected test has no execution evidence', async () => {
  expect(false).toBe(true);
});
