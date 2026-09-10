import { test } from "@playwright/test";

test("stays active long enough to exercise cancellation", async () => {
  await new Promise((resolve) => setTimeout(resolve, 30_000));
});
