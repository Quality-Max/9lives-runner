import {test as base, expect} from '@playwright/test';
import {engineIdentity} from './protocol';
import {executeGoal, type GoalOptions, type GoalResult} from './goal';
export type {GoalOptions, GoalResult} from './goal';

export type NineLives = {
  /** Execute a named step; assertions and browser handles remain ordinary Playwright. */
  goal(instruction: string, options?: GoalOptions): Promise<GoalResult>;
  step<T>(name: string, body: () => Promise<T>): Promise<T>;
};

export const test = base.extend<{nineLives: NineLives}>({
  nineLives: async ({page}, use, testInfo) => {
    engineIdentity();
    await use({goal: (instruction, options) => base.step('9lives goal', () => {
      if (testInfo.retry > 0) throw new Error('9lives goal stopped: automatic goal retries disabled');
      return executeGoal(page, instruction, options);
    }), step: (name, body) => base.step(name, body)});
  },
});
export {expect};
export {protocolVersion} from './protocol';
