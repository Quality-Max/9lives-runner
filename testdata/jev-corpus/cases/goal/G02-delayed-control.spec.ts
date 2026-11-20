import {test, expect} from '@9l/playwright';

test("G02-delayed-control", async ({page, n9l}) => {
  await page.setContent(`<label>Name <input aria-label="Name"></label><button>Continue</button><p role="status">Ready</p><script>document.querySelector("button").onclick=()=>{document.querySelector("button").remove();setTimeout(()=>{const b=document.createElement("button");b.textContent="Confirm cart";b.onclick=()=>{document.querySelector("[role=status]").textContent="Order confirmed"};document.body.appendChild(b)},350)}</script>`);
  const result = await n9l.goal('Fill Name using parameter name, continue, then confirm the cart.', {params: {name: 'Fixture Person'}});
  expect(result.verified).toBe(false);
  await expect(page.getByRole('status')).toHaveText('Order confirmed');
});
