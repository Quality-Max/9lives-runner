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
   that the status says “Order placed”. See the actual screenshot and pass/fail result.
3. **Ask Jev to look**, when connected. See its answers to two questions and, with
   images enabled, its predicted button outline beside the browser’s measured outline.

The default works offline, with **zero provider requests** and no key. It shows
no canned AI answers. You can try both payments, toggle the button outline and
open the saved execution evidence. Light/dark theme follows the existing 9lives demo.

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
Each explicit “Ask Jev” click makes **one paid-provider request**. The server
allows one check at a time, at most six live requests and 24 total checks per
session. Failed or cancelled attempts consume their slot. Restart for a new session.
There are no automatic retries.

`--images` includes only this demo’s synthetic screenshot and a Box question.
Omit it for two text-only Noul questions. Select another supported model with
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

Each command captures the selected fixed checkout once and prints a saved HTML
report path. The last command requires the key and makes one provider request.
Provider/model/usage failures return exit 2 with a sanitized report status.
`go run ./demo/jev --images` exercises image preparation without a provider call.

## Evidence and limits

The browser uses a controlled page with no external resources and blocks network
requests during capture. Its confirmation assertion fails for the broken payment
and passes for the working payment. Both outcomes are exercised in the smoke.
The page shown to the visitor and the fresh test browser use the same embedded
fixture and selected scenario; the saved capture belongs to the executed check.

Live text mode sends the synthetic accessibility observation and owned questions
to TypeSafe. Image mode adds its prepared PNG and experimental image header.
There is no arbitrary URL, screenshot-path or prompt input. Each request has a
20-second deadline, one-request cap, three-question cap, 2 MiB request budget and
zero retries. Actual available token usage is reported; no billing-cost cap is
invented. Cancelling the browser request cancels the owned execution context.

Each check saves `.context/jev-demo-*/checkout.png`, `record.json` and `index.html`.
These known synthetic reports show bounded probabilities and transformed Box
coordinates. They retain no raw provider envelope, snapshot, prompt or error body.
The safe record identifies the checkout revision and exact scenario fixture hash.
The browser UI receives the assertion result, image, bounded judgments and safe
record; credentials and raw accessibility snapshots are excluded.

Jev’s probabilities and Box are inferences. They never change the assertion,
click an inferred control, heal a test or create a passing runner receipt. This
implements the QUA-2228 foundation demonstration; diagnosis, verified locator
repair and goal actions remain separate tickets. See the [contract](../../docs/jev.md).

```sh
go test ./demo/jev
npm run smoke:jev
```

The smoke starts its own offline server, executes both actual browser outcomes,
checks saved reports, mobile layout, theme persistence and offline continuation.
A separate, explicitly synthetic UI fixture checks answer/Box presentation;
it never contacts TypeSafe and does not qualify model accuracy.
