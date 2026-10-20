import type { Page, TestInfo } from '@playwright/test';

export type Observation = {
  initialRecordIds: string[];
  events: { action: 'editor-opened'; recordId: string }[];
};

// Fully intercepted synthetic app. The app reports the clicked row's ID;
// the test's expected ID and its pass/fail verdict never enter this log.
export async function installRecords(page: Page): Promise<Observation> {
  const control = process.env.NINELIVES_TARGET_CONTROL || 'baseline';
  if (!['baseline', 'wrong-row', 'reordered', 'setup-failure', 'timeout'].includes(control)) {
    throw new Error('Unknown target fixture control');
  }
  if (control === 'setup-failure') throw new Error('Controlled fixture setup failure');
  if (control === 'timeout') {
    // The caller's test budget terminates this owned negative control.
    await new Promise(() => {});
  }
  const initialRecordIds = control === 'reordered' ? ['record-b', 'record-a'] : ['record-a', 'record-b'];
  const observation: Observation = { initialRecordIds, events: [] };
  await page.route('http://records.test/**', async route => {
    if (route.request().method() === 'POST' && new URL(route.request().url()).pathname === '/opened') {
      const { recordId } = route.request().postDataJSON();
      if (!initialRecordIds.includes(recordId)) throw new Error('Unknown synthetic record');
      observation.events.push({ action: 'editor-opened', recordId });
      await route.fulfill({ json: { recorded: true } });
      return;
    }
    await route.fulfill({ contentType: 'text/html', body: `
      <table aria-label="Records"><tbody>${initialRecordIds.map(id => `
        <tr data-record-id="${id}"><td>${id}</td><td><button><span>Edit</span></button></td></tr>
      `).join('')}</tbody></table>
      <section role="dialog" aria-label="Record editor" hidden><output data-testid="editor-record-id"></output></section>
      <script>
        for (const button of document.querySelectorAll('button')) button.onclick = async () => {
          const recordId = button.closest('tr').dataset.recordId;
          document.querySelector('[data-testid="editor-record-id"]').textContent = recordId;
          document.querySelector('[role="dialog"]').hidden = false;
          await fetch('/opened', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({recordId})});
          document.querySelector('[role="dialog"]').dataset.auditComplete = 'true';
        };
      </script>` });
  });
  await page.goto('http://records.test/');
  return observation;
}

export async function attachObservation(page: Page, info: TestInfo, observation?: Observation) {
  if (!observation) return;
  // Wait for the app's audit request to finish even when the weak assertion
  // passes immediately. Only synthetic IDs and action names are attached.
  await page.locator('[role="dialog"][data-audit-complete="true"]').waitFor({ state: 'attached' });
  await info.attach('target-observation', {
    body: JSON.stringify(observation), contentType: 'application/json',
  });
}
