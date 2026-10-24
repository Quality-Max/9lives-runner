# Jev: try the shop, check the result, ask AI

A guided, interactive local demo: buy a pretend notebook, check whether the order
worked, then optionally ask Jev for a second opinion. Switch between **Broken**
and **Working** payments to see the difference. No real card or payment is involved.

## Start the interactive demo

From the repository root, with Go and Node installed:

```sh
npm ci
npm exec playwright install chromium
npm run demo:jev
```

The command opens `http://127.0.0.1:4175` in your browser. Press Ctrl+C in the
terminal to stop. For a different port, use `npm run demo:jev -- --port 0`.
To start without opening a browser, run `go run ./demo/jev --serve`.
On Linux, install the browser with `npm exec playwright install --with-deps chromium`.

1. **Try the payment.** Click “Place order” in the shop, or the large guided button.
2. **Check with 9lives.** A real Playwright browser repeats the checkout and asserts
   that the shop saved one notebook order with the right item, quantity and total.
   See the actual screenshot and pass/fail result.
3. **Ask Jev to look**, when connected. See its answers to two questions and, with
   images enabled, its predicted button outline beside the browser’s measured outline.

The default works offline, with **zero provider requests** and no key. It shows
no canned AI answers. You can try both payments, toggle the button outline and
open the saved execution evidence. Light/dark theme follows the existing 9lives demo.

## Play The Great Little Hunt

Open **[the game](http://127.0.0.1:4175/cats)**, or follow the game link from the
shop. Press **Let’s play**. Finish a level to unlock the next:

1. **Cats:** nine sleepy cats in a cozy room.
2. **Dogs:** nine smaller dogs scattered around a park.
3. **Mice:** six tiny mice among pantry jars and three cheese decoys.
4. **Striped stranger:** one person in a crowd, with a red-and-white striped shirt
   and hat, round glasses and blue trousers.

Play untimed, or select **Race the clock** before starting a level. Each level
has a shorter countdown (75, 60, 50 and 40 seconds). Retry a level when time runs
out. Zoom in for small figures, or use Tab and Enter/Space on the hiding spots.
**Show hiding places** is practice and cannot unlock the next level.

### Watch 9lives play

Press **Watch 9lives play** to watch a separate browser round. Your game and timer
pause. Without Jev configured, the **Free demo** uses the known synthetic answer map,
costs nothing and makes no model request. The introduction explains Jev’s visual
judgment and 9lives’ browser actions and score checks before the first run. Turn on
**Plant a bug: one find earns no point** to restart the current level with a broken
click handler in both your game and 9lives’ round. Find all nine cats yourself: the
score sticks at 8/9, and clicking the affected cat explains the missed point.
Watch 9lives test the same defect: the score assertion fails. Switch the bug off
to restart a working level. Watching alone preserves your own score and timer.

This button runs an ordinary Playwright spec through the **existing Go 9lives
runner**, with one job, one attempt, no retries and a bounded deadline. It checks:

- The selected observed spots match every requested figure and no lookalike.
- Each correct click adds exactly one to the score.
- A repeated click adds nothing, and lookalikes add nothing.
- The final score equals the number of requested figures.

The **Clicks and checks** log opens automatically beneath the game picture and
shows actual browser progress alongside the screenshots. Scroll the log to review
earlier steps. **See the saved test result** opens the runner’s saved JSON summary, including the executed-test
count, validation status and any failed assertion. Passing is based on the
runner receipt, not a model claim or screenshot caption. Ordinary Playwright
assertion coverage remains `unknown`; this is not independent semantic coverage.
Stop cancels the owned runner attempt and cannot produce a passing result.

With `--live --images` and a TypeSafe key, the button automatically uses Jev.
The panel shows **Live Jev**. The three steps above the game explain: Jev looks,
9lives clicks, and 9lives checks. Opening the page never sends a request.
One explicit run sends a 3 × 3 contact sheet of enlarged crops from one fresh
screenshot and nine independent yes/no (Noul) questions, one per observed control.
Each labelled tile shows the original pixels and a small rim of scene context,
without redrawing or isolating figures. Questions name the tile and requested
visual features. The tile IDs map back to the original observed controls; clicks
use those controls and recheck their geometry, never contact-sheet coordinates.
The answer map, correct count, player score and broken-click flag are withheld.
Every probability must be at least 0.7 (yes) or at most 0.3 (no);
uncertain decisions stop. The test checks the proposed set against the
synthetic oracle before clicking. Score checks run independently afterward.

This fixed example demonstrates real runner execution with bounded Jev choices.
It is **not the general Jev goal provider**: navigation, filling forms and generic
goal actions remain QUA-2232. The final
scene is an original illustration, not artwork from a commercial puzzle book.

Local live checks on 24 October 2026 with `jev-1.14.0` and enlarged crops:
all four healthy levels passed complete runner executions — cats 9/9, dogs 9/9,
mice 6/6 (excluding three cheese decoys), and the striped stranger 1/1. A
separate run with the missing-point bug failed on its score assertion for each
level. All eight runs produced validated receipts with one executed test and
no retries. The earlier full-picture observation stopped on uncertain dog and
crowd judgments; enlarged crops resolved these checks without changing the
confidence thresholds or assertions. These are individual synthetic checks,
not an accuracy benchmark or a guarantee of future model decisions. The page
explains stopped selection as **Jev needs your help**, while retaining the
failed receipt and keeping score failures distinct.

The earlier standalone picture reports remain available (including the bonus
cats-versus-cushions illustration):

```sh
go run ./demo/jev --scenario cats --images
go run ./demo/jev --scenario mice --images
go run ./demo/jev --scenario striped --live --images
```

## Connect Jev for real answers

Stop the demo, then supply `TYPESAFE_API_KEY` to the local Go process. In an
interactive zsh terminal, this keeps the value out of command history and hides
it while typing:

```zsh
read -rs 'TYPESAFE_API_KEY?TypeSafe API key: '
export TYPESAFE_API_KEY
npm run demo:jev -- --live --images
unset TYPESAFE_API_KEY
```

Opening the page and running “Check with 9lives” still make **no provider request**.
Each explicit “Ask Jev” click, or a hunt run in Live Jev mode, makes **one paid-provider request**. The server
allows one check at a time, at most six live requests and 24 total checks per
session. Failed or cancelled attempts consume their slot. Restart for a new session.
There are no automatic retries.

`--images` allows only this demo’s synthetic screenshots. Single-report and shop
requests include a Box question; the game runner uses bounded Noul questions.
Omit it for the shop’s two text-only Noul questions. Select another supported model with
`--model`; the default is `jev-1.14.0`. Missing credentials fail before starting
the server or browser capture. Live availability and visual accuracy require
real qualification; offline tests do not establish either.

The key stays in Go. It is never entered into the webpage or sent to Node or
the browser. The server binds only to loopback and checks the request origin,
host and a session nonce before accepting a check. This is a local presenter
demo, not a publicly hosted service.

## Save a single report without the interactive server

```sh
go run ./demo/jev
go run ./demo/jev --scenario success
go run ./demo/jev --live --images
```

Each command captures the selected fixed scene once and prints a saved HTML
report path. The last command requires the key and makes one provider request.
Provider/model/usage failures return exit 2 with a sanitized report status.
`go run ./demo/jev --images` exercises image preparation without a provider call.

## Evidence and limits

The browser uses a controlled page with no external resources and blocks network
requests during capture. There is no “Order placed” or “Order not placed” status
label. Customers see only “Payment declined. Try another card.” or “Thank you! Your
notebook is on its way.” Jev judges the meaning of those messages.

The browser independently reads a separate in-memory order list. It verifies the
list is initially empty, clicks the payment button and asserts exactly one saved
notebook order with the expected quantity and total. The assertion fails for the
broken payment and passes for the working payment, without matching any message.
This list is a toy browser store that resets on reload, not a real backend. Both
outcomes are exercised in the smoke, including changed and misleading messages.
The page shown to the visitor and the fresh test browser use the same embedded
fixture and selected scenario; the saved capture belongs to the executed check.

Live text mode sends the synthetic accessibility observation and owned questions
to TypeSafe. It never sends the order list, scenario identifier or browser verdict
as model state; the failure-class hint is left empty for both outcomes. Image
mode adds its prepared PNG and experimental image header.
There is no arbitrary URL, screenshot-path or prompt input. Each shop request has a
20-second deadline, one-request cap, three-question cap, 2 MiB request budget and
zero retries. Actual available token usage is reported; no billing-cost cap is
invented. Cancelling the browser request cancels the owned execution context.

Each check saves `.context/jev-demo-*/checkout.png`, `record.json` and `index.html`.
These known synthetic reports show bounded probabilities and transformed Box
coordinates. They retain no raw provider envelope, snapshot, prompt or error body.
The safe record identifies the checkout revision and exact scenario fixture hash.
The browser UI receives the assertion result, image, bounded judgments and safe
record; credentials and raw accessibility snapshots are excluded.

If the page lacks meaningful clues, its appearance cannot establish that an order
exists. Noul probabilities near 0.5 express uncertainty; they are not a passing
verdict. Jev’s probabilities and Box are inferences. In the shop they never change the
assertion, click a control, heal a test or create a passing runner receipt. The
game can click a finite observed spot only after its proposed set passes the
independent synthetic oracle check. This
implements the QUA-2228 foundation demonstration; diagnosis, verified locator
repair and goal actions remain separate tickets. See the [contract](../../docs/jev.md).

```sh
go test ./demo/jev
npm run smoke:jev
```

The smoke starts its own offline server, executes both checkout outcomes and
all four game levels, passing and deliberately failing runner receipts,
cancellation, progressive unlock, repeat taps, decoys, practice mode, the optional
timer and mobile layouts. It also checks saved reports, theme persistence and
offline continuation.
A separate, explicitly synthetic UI fixture checks answer/Box presentation;
it never contacts TypeSafe and does not qualify model accuracy.
