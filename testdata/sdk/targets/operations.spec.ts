import {createTargetTest} from '@9l/playwright';
import {fakeFinance} from './fake-finance';

const mode = process.env.NINELIVES_TARGET_CASE ?? 'healthy';
const web = process.env.NINELIVES_TARGET?.startsWith('web:');
const test = createTargetTest({
  web: {
    open: async page => {
      await page.setContent(`<button data-testid="button" style="position:absolute;left:40px;top:40px;width:200px;height:80px" onclick="document.querySelector('[data-testid=status]').textContent='tapped'">Tap</button>
        <div data-testid="list" style="position:absolute;left:0;top:160px;width:400px;height:600px;overflow:auto"><div style="height:3000px"></div></div>
        <output data-testid="status" style="position:absolute;top:780px"></output>`);
      await page.evaluate(() => {
        // A click anywhere but the button dismisses the "modal" the button opened.
        document.addEventListener('click', event => { if (!(event.target as Element).closest('[data-testid=button]')) document.querySelector('[data-testid=status]')!.textContent = ''; });
        document.querySelector('[data-testid=list]')!.addEventListener('scroll', () => { document.querySelector('[data-testid=status]')!.textContent = 'scrolled'; });
        window.addEventListener('popstate', () => { document.querySelector('[data-testid=status]')!.textContent = 'back'; });
        history.pushState(null, '', '#second');
      });
    },
    reset: async page => { await page.evaluate(() => { document.querySelector('[data-testid=status]')!.textContent = ''; }); },
  },
  native: target => fakeFinance(target, mode),
}).extend({
  ...(web ? {} : {
    page: async () => { throw new Error('native requested page'); },
    context: async () => { throw new Error('native requested context'); },
    browser: async () => { throw new Error('native requested browser'); },
  }),
});

// Portable operations run on both targets with the same assertions; the
// Android-only ones run on the native target and fail with a stable reason on web.
test('geometry, touch, scrolling and back are portable', async ({app}) => {
  await app.open();
  const viewport = await app.viewport();
  const button = await app.getByTestId('button').bounds();
  if (!(viewport.width > 0 && viewport.height > 0 && button.width === 200 && button.height === 80)) throw new Error('unexpected geometry');
  await app.tapAt({x: button.x + Math.floor(button.width / 2), y: button.y + Math.floor(button.height / 2)});
  // @9l-outcome tapped-by-coordinates
  await app.getByTestId('status').toHaveText('tapped');
  await app.getByTestId('list').scroll('down', {percent: 0.5});
  await app.getByTestId('status').toHaveText(web ? 'scrolled' : 'scrolled down 0.5');
  await app.getByTestId('absent').toBeHidden();
  // Two digests of a settled screen agree; neither image leaves the worker.
  const settled = await app.screenDigest();
  if (settled !== await app.screenDigest() || !/^[a-f0-9]{64}$/.test(settled)) throw new Error('screen digest unstable');
  await app.back();
  await app.getByTestId('status').toHaveText('back');
});

test('keyboard and process lifecycle are native only', async ({app}) => {
  await app.open();
  if (mode === 'unsupported' || web) {
    await app.terminate();
    return;
  }
  await app.getByTestId('amount').fill('7');
  await app.toHaveKeyboard(true);
  await app.hideKeyboard();
  await app.toHaveKeyboard(false);
  await app.terminate();
  await app.getByTestId('status').toBeHidden();
  await app.activate();
  // @9l-outcome draft-survives-restart
  await app.getByTestId('status').toHaveText('draft 7');
});

// A modal is opened and seen before the outside touch that should dismiss
// it, so the hidden assertion protects the dismissal: an ignored touch fails
// it. In the observer control the dialog's window is gone but the snapshot
// still lists it, and the driver classifies the disagreement.
test('an outside touch dismisses the modal', async ({app}) => {
  await app.open();
  const button = await app.getByTestId('button').bounds();
  await app.tapAt({x: button.x + 10, y: button.y + 10});
  await app.getByTestId('status').toBeVisible();
  const viewport = await app.viewport();
  await app.tapAt({x: viewport.width - 10, y: viewport.height - 10});
  // @9l-outcome modal-dismissed-by-outside-touch
  await app.getByTestId('status').toBeHidden(300);
});

// A touch at or beyond the viewport edge is rejected before dispatch.
test('a touch outside the viewport is refused', async ({app}) => {
  await app.open();
  const viewport = await app.viewport();
  await app.tapAt({x: viewport.width, y: 0});
});

// A driver that answers visibility with anything but a boolean proves nothing.
test('a malformed visibility result is not hidden', async ({app}) => {
  await app.open();
  await app.getByTestId('status').toBeHidden();
});
