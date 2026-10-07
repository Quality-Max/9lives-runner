# Working on 9lives

Read and follow [AGENTS.md](AGENTS.md) for architecture, delivery, validation and
mandatory secret-handling rules. They apply to every coding agent in this repo.

Our intention is a developer-first testing framework: ordinary assertions,
bounded goal-driven execution, independently verified outcomes, deterministic
replay of verified actions and trustworthy healing. It must run locally without
an account and integrate with the QualityMax platform through shared contracts.

The Go core owns execution policy and evidence. `@9lives/playwright` owns the
TypeScript fixtures and browser work. Browser handles stay in Playwright workers;
attempt identity, budgets and process-tree cancellation stay in Go. Maintain
strict versioned boundaries and preserve existing specs.

The current SDK supports `nineLives.step` and bounded `nineLives.goal` with
`9l run --sdk`, an explicit HTTP or offline scripted provider, and ordinary
Playwright assertions. Named values stay in browser workers; credentials and
attempt budgets stay in Go. Model completion never proves a test passed.
Independent behavioral verification, verified replay, mobile execution and
production benchmark qualification remain roadmap work. Do not describe proposed capabilities as shipped. Reuse the platform's
grounding, generation, healing and mobile contracts rather than rebuilding them.

Deliver changes with observable execution evidence. Incomplete or unverified
runs must stay unsuccessful; healing must preserve intent and assertions.
Protocol step categories do not prove assertion coverage. New AI or replay
paths must earn correctness, latency and cost claims through measured runs.

Useful checks:

```sh
go test -race ./...
go vet ./...
npm ci
npm test
npm exec playwright install chromium
npm run smoke
```

Use the current Conductor branch and `origin/main`. Do not publish packages,
merge or deploy merely to complete local implementation work. Save local
review evidence in `.context/` and update Linear with accurate progress.
