import {test, expect} from '@9l/playwright';
test('identical buttons the goal cannot tell apart stop the goal', async ({page, n9l}) => {
  await page.setContent(`<p role="status">Ready</p>
    <form><button type="button" onclick="document.querySelector('[role=status]').textContent='First saved'">Submit</button></form>
    <form><button type="button" onclick="document.querySelector('[role=status]').textContent='Second saved'">Submit</button></form>`);
  await expect(n9l.goal('Submit the form.')).rejects.toThrow('ambiguous_target');
  await expect(page.getByRole('status')).toHaveText('Ready');
});
