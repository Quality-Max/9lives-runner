import {test, expect} from '@9lives/playwright';

test('caught invalid or unconfigured goal still fails engine', async ({nineLives}) => {
  await expect(nineLives.goal('Continue', process.env.NINELIVES_SMOKE_INVALID_GOAL ? {maxActions: 0} : {})).rejects.toThrow();
  expect(true).toBe(true);
});
