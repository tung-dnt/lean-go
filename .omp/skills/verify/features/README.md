# lean-go browser verification map

Run `just browser` from a checkout prepared with `just install`, with Docker available or an isolated
test database configured. The harness supplies an empty schema, a synthetic admin and a fresh browser
context per test. Do not use a running development instance or run two suites in the same checkout.

- [Authentication](authentication.md): login errors/success, hover-safe logout and logged-out history.
- [Notes](notes.md): validation, Unicode counting, persistence and admin deletion; there is no edit UI.
- [Navigation](navigation.md): repeated htmx navigation and Alpine initialization after swaps/history.

The executable source of each journey is `scripts/browser.test.ts`. Run the whole gate with
`just browser`; require three passing tests and inspect screenshots under `test-results/`.
The HTML report and failure traces survive database/server teardown. Report inaccessible paths as
failures, not skipped successes. This map covers Chromium only, not Safari or Firefox.
