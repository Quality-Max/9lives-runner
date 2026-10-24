# Jev: an illustrative local request

This example exercises the J1 client on one fixed synthetic checkout. Playwright
clicks “Place order”, observes “Payment declined” and executes an order-confirmation
assertion that fails as intended. The optional Jev request asks two independent
Noul questions over the same observation. With image opt-in it also asks for a
Box around “Try again”, shown alongside the actual Playwright control bounds.
It does not implement the later diagnosis, repair or goal-action tickets.

Run from the repository root with Go, Node and Chromium:

```sh
npm ci
npm exec playwright install chromium
go run ./demo/jev
```

The default is offline: **zero API requests**, no key needed, and no canned
model result. Open the printed `.context/jev-demo-*/index.html` to see the actual
browser screenshot, expected assertion failure and disabled decision record.
`go run ./demo/jev --images` also exercises image preparation locally.

## Try one live request

Supply `TYPESAFE_API_KEY` in the local Go process environment. For an interactive
zsh terminal, this avoids placing the value in command history or echoing it:

```zsh
read -rs 'TYPESAFE_API_KEY?TypeSafe API key: '
export TYPESAFE_API_KEY
go run ./demo/jev --live
unset TYPESAFE_API_KEY
```

`--live` makes **one paid-provider request**, with two text questions. To include
only this demo's synthetic screenshot and the third, Box question, use:

```sh
go run ./demo/jev --live --images
```

Keep the key available for that command too. Select another supported model
explicitly with `--model`; the default is `jev-1.14.0`. Missing credentials fail
before browser capture or any API call. HTTP/model/usage failures return exit 2;
inspect the sanitized status in the report. Availability and visual accuracy
require actual qualification; this example does not claim either.

## What leaves the machine and what is saved

Offline mode contacts no provider. The controlled page has no external resources
and its browser network is blocked. Node and browser processes receive an explicit
environment allowlist that excludes provider credentials.

Live text mode sends the fixed synthetic accessibility observation and owned
questions to TypeSafe. `--images` adds the prepared synthetic PNG and experimental
image header. There is no arbitrary URL, screenshot-path or prompt input. The
request has a 20-second deadline, one-request cap, three-question cap, 2 MiB
request budget and zero retries. Actual available token usage is reported; no
token or billing-cost cap is invented.

Each run creates a new `.context/jev-demo-*` directory containing `checkout.png`,
the safe `record.json` and `index.html`. The illustrative report opts into
displaying bounded probabilities and transformed Box coordinates for this known
synthetic page. It saves no raw provider envelope, snapshot, prompt or error body.
The record identifies the checkout revision and exact embedded fixture hash.
No customer page, account, real payment or order is involved.

The failed assertion is an observed business outcome. Jev's probabilities and
Box are inferences. The example never clicks an inferred Box, heals a test or
creates a passing runner receipt. See the [foundation contract](../../docs/jev.md).
