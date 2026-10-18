# Constraints

This is the quality bar for people and AI agents. Do not weaken it to make a change pass: tightening is silent, loosening needs the owner's explicit OK in the PR. How the code is cut is [LEANGO.md](LEANGO.md); how to write each part is [docs/conventions](docs/conventions/); [AGENTS.md](AGENTS.md) holds the Floor and says which of those to read for which change.

When you start a project from this template, put your name and the date on the line below, and fill in the Measured and Exceptions tables as the project gets real numbers.

Last reviewed: _(date)_ by _(owner)_.

## Floor (always enforced)

The Floor is in [AGENTS.md](AGENTS.md), so that every agent has it loaded from its first turn. It is part of this bar and is held to the same rule: it is not weakened in the same PR as the change that was failing.

## Enforced with numbers

| Dimension | Rule | Checked by | Runs at | Blocks |
|---|---|---|---|---|
| Build | Binary, CSS and JS build | `just build` | task end, CI | yes |
| Tests | All pass, race detector clean | `just test` (builds `main.css` and `app.js`, then `go test -race -coverprofile=cover.out ./...`) | task end, CI | yes |
| SQL | Generated code up to date, queries vetted | `just sqlc-verify` (regenerates `internal/external/db` and fails on a diff), `just sqlc-vet` | task end, CI | yes |
| Lint | No findings | `just lint` (`golangci-lint run`) | task end, CI | yes |
| Go vulnerabilities | No called vulnerability | `just vuln` (`go tool govulncheck ./...`) | task end, CI | yes |
| Types (TS) | Zero errors | `just types` (`bun run typecheck`) | every edit to `web/js`, CI | yes |
| Secrets | No findings, over the whole history | `just secrets` (gitleaks v8.30.1); a test fixture is ignored by fingerprint in `.gitleaksignore` | task end, CI | yes |
| Dependencies | No known vulnerability (Go + npm) | `just deps` (osv-scanner v2.7.0) | task end, CI | yes |
| Duplicate code | No clone of 70 tokens or more in Go, TS or CSS (tests and sqlc output excluded); move shared code into one place (`pkg/`, the owning domain, a layout helper) instead of copying it | `just dupes` (`jscpd` 4.3.0 with `.jscpd.json`, threshold 0) | task end, CI | yes |

`just check` runs every row above. CI (`.github/workflows/test.yml`) runs one named step per row, each even when an earlier one failed, so one run shows everything that is wrong. The scanners run pinned through `go run` and jscpd through `bun x`, so none of them is in `go.mod` or `bun.lock`. Commands run through [just](https://github.com/casey/just) (`brew install just`), from the `justfile`.

## Measured (ratchets)

| Metric | Today | Direction | Why this number |
|---|---|---|---|
| Go statement coverage | _(measure on the first real domain)_ | must not fall more than 0.5 points | Measured, not invented |
| `main.css` gzip | _(measure)_ | ≤ +10% per change | Utility CSS should stay small |
| `js/app.js` gzip | _(measure)_ | ≤ +10% per change | htmx + Alpine dominate; growth should be deliberate |

## Exceptions

| ID | Rule | Path | Reason | Owner | Expires |
|---|---|---|---|---|---|
