import {createTargetTest} from '@9l/playwright';

// Conditional rendering replaces the disabled Send with a new enabled node
// 30ms after input. Actions must follow the replacement, not the stale node.
const test = createTargetTest({web: {
  open: async page => { await page.setContent(`<input data-testid="amount"><span id="slot"><button data-testid="send" disabled>Send</button></span><output data-testid="receipt"></output><script>
    document.querySelector('input').addEventListener('input', () => setTimeout(() => {
      document.getElementById('slot').innerHTML = '<button data-testid="send">Send</button>';
      document.querySelector('[data-testid=send]').onclick = () => { document.querySelector('output').textContent = 'Sent $' + document.querySelector('input').value; };
    }, 30));
  </script>`); },
  reset: async () => {},
}});

test('a re-rendered control stays addressable', async ({app}) => {
  await app.open();
  await app.getByTestId('amount').fill('5');
  await app.getByTestId('send').tap();
  await app.getByTestId('receipt').toHaveText('Sent $5');
});
