import {test, expect} from '@9l/playwright';

test('caught invalid or unconfigured goal still fails engine', async ({n9l}) => {
  await expect(n9l.goal('Continue', process.env.NINELIVES_SMOKE_INVALID_GOAL ? {maxActions: 0} : {})).rejects.toThrow();
  expect(true).toBe(true);
});
