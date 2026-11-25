---
name: verify
description: Verify lean-go's web UI in real Chromium with the real server and isolated PostgreSQL; use after changes to views, htmx, Alpine, auth, assets or navigation.
---

# Verify lean-go

## Launch

From the repository root, run `just install` once. Keep Docker running, then run `just browser`.
This builds the embedded CSS/JavaScript and runs the browser-tagged Go test. Its existing server
fixture starts PostgreSQL, creates and migrates a random schema, picks a loopback port, waits for
`/healthz`, and seeds a synthetic admin. Playwright receives that instance's URL and credentials.
No development server, development account or manual seeding is used. Do not run Playwright directly.

CI supplies `GO_DATABASE_TESTCONNECTION` instead of starting a testcontainer. An override must
point to a disposable test database and include the literal `search_path=test_$RAND`; never pass
the development connection string. Each run has its own schema, port and browser context.
The runner owns `test-results/` and `playwright-report/`, so do not run it concurrently in one checkout.

## Doctor

Before driving a suspect setup, run these read-only checks:

```sh
docker info --format '{{.ServerVersion}}'
bun -e 'import { existsSync } from "node:fs"; import { chromium } from "playwright"; const executable = chromium.executablePath(); if (!existsSync(executable)) throw new Error("Run just browser-install"); console.log(executable)'
```

The fixture refuses startup unless the real server's readiness endpoint answers. Its request log
prints the owned loopback URL and shutdown result. A missing browser is an error, never a skipped test.

## Drive

Run `just browser`; the three journeys in `scripts/browser.test.ts` drive real forms and links.
Read [the feature map](features/README.md) for entry points and expected results. Tests use accessible
names, form IDs and rendered counters. No API mocks, cookie injection, browser-state setters or retries.
The hover check observes a bounded request timeout: a speculative `/logout` request must not occur.

## Evidence

Require exit code zero and all three Playwright journeys passing, with no browser-health errors.
Screenshots remain under `test-results/`; `playwright-report/index.html` records the journeys.
Failures also retain traces. Inspect a screenshot as well as the assertions when changing UI.
Use `bunx --bun playwright show-report` for the report or `bunx --bun playwright show-trace PATH`
for a trace. CI uploads both directories as `browser-evidence` on failure.

Creation is proved by a reload and reopening the note from the list; deletion by a reloaded list
without the note. Navigation tests compare the document's time origin so plain HTML reloads cannot
stand in for functioning htmx. HTTP tests alone do not execute Alpine or prove browser history.

## Cleanup

The Go fixture shuts down its server, drops its schema and terminates its testcontainer after the
browser exits, including assertion failures. Playwright closes its browser contexts. Do not kill
processes by name, drop other schemas, or delete evidence during cleanup. Confirm screenshots and
`playwright-report/index.html` still exist after the command returns. Each later run replaces the
runner's report, so copy evidence outside these directories before another run if it must be retained.
