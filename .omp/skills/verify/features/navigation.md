# Navigation

Sidebar navigation swaps server-rendered content through htmx, and Alpine remains usable after repeated swaps and browser history restoration.

## Sub-features

- Repeated login/cancel navigation keeps the same browser document.
- Reinserted note forms initialize Alpine and count input correctly.
- Back/forward restores usable forms, not stale or inert markup.
- Every journey fails on JavaScript exceptions, console errors or failed static assets.

## How to get to it (user POV)

Open `/`, choose sidebar `Log in`, then `Cancel` in the login form. Use browser back/forward controls.

## Driving it with Playwright

Preconditions: the `just browser` fixture owns the server; start this journey anonymous.
Run `just browser`; journey `repeated htmx navigation and history reinitialize Alpine`:

- Choose `Log in`, then `Cancel`, twice. Each return shows the `Notes` heading.
- Enter `Back 🌍` in `Note`; the counter reads `6 / 5000` after each swap.
- The document's time origin must stay unchanged; a full reload cannot pass as htmx navigation.
- Go back to the login form, then forward to the note form. Enter `Forward 🌍`; the counter reads `9 / 5000`.

## Gotchas

Use actual links and browser history. Do not insert controls or set Alpine/htmx state through scripts.
Screenshots prove rendering; the counter and document assertions prove executable behavior.
