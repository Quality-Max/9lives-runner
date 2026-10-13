import {start} from '../app/server.cjs';
import type {test as sdkTest} from '@9l/playwright';

// Keep test/expect imported directly in each spec so advisory assessment can
// identify them. The app starts from that spec's checkout, including in confirm.
export function useShop(test: typeof sdkTest) {
  let app: Awaited<ReturnType<typeof start>>;
  test.beforeAll(async () => {
    app = await start({port: Number(process.env.LAB_PORT || 0), profileBug: process.env.LAB_PROFILE_BUG === '1'});
  });
  test.afterAll(async () => { if (app) await app.close(); });
  return () => app.url as string;
}
