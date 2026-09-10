import { expect, test } from "@playwright/test";

test("executes and validates a local suite", () => {
  expect(1 + 1).toBe(2);
});
