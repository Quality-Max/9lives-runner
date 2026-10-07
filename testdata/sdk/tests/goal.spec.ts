import {test, expect} from '@9lives/playwright';
test('bounded goal handles delayed controls and style drift', async ({page, nineLives}) => {
  await page.setContent(`<label>Name <input aria-label="Name"></label><button class="old">Continue</button><p role="status">Ready</p><script>
  document.querySelector('button').onclick = () => {
    document.querySelector('button').remove();
    setTimeout(() => {
      const button = document.createElement('button'); button.textContent = 'Confirm cart'; button.className = 'new-style';
      button.onclick = () => { document.querySelector('[role=status]').textContent = ${process.env.NINELIVES_SMOKE_DEFECT === '1' ? "'Declined'" : "'Order confirmed'"}; }; document.body.appendChild(button);
    }, 350);
  };
  </script>`);
  const result = await nineLives.goal('Fill Name using parameter name, continue, then confirm the cart.', {params: {name: 'Fixture Person'}});
  expect(result.verified).toBe(false);
  await expect(page.getByRole('status')).toHaveText('Order confirmed');
});
