import {test as base, expect} from '@playwright/test';
import {engineIdentity} from './protocol';
import {executeGoal, type GoalOptions, type GoalResult} from './goal';
import {instrument, openProveChannel} from './prove';
export type {GoalOptions, GoalResult} from './goal';

export type NineLives = {
  /** Execute a named step; assertions and browser handles remain ordinary Playwright. */
  goal(instruction: string, options?: GoalOptions): Promise<GoalResult>;
  step<T>(name: string, body: () => Promise<T>): Promise<T>;
};

export const test = base.extend<{n9l: NineLives}>({
  // Outside `9l prove` this passes the context through unchanged. Overriding
  // context, not adding an auto fixture, keeps tests that never use a browser
  // from launching one.
  context: async ({context}, use, testInfo) => {
    const channel = openProveChannel(process.env, testInfo);
    try {
      if (channel) await instrument(context, channel);
      await use(context);
    } finally {
      channel?.close();
    }
  },
  n9l: async ({page}, use, testInfo) => {
    engineIdentity();
    await use({goal: (instruction, options) => base.step('9l goal', () => {
      if (testInfo.retry > 0) throw new Error('9lives goal stopped: automatic goal retries disabled');
      return executeGoal(page, instruction, options);
    }), step: (name, body) => base.step(name, body)});
  },
});
export {expect};
export {protocolVersion} from './protocol';
