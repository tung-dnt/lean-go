# Notes

A visitor posts notes; the admin can delete them. The form reports validation errors and counts Unicode code points through Alpine.

## Sub-features

- Empty titles are rejected without losing the body or its counter.
- Alpine counts astral Unicode characters correctly before and after an htmx form swap.
- Saved notes survive reload and reopening from the list.
- Admin deletion survives a list reload.

## How to get to it (user POV)

Open `/` for the `Title` and `Note` fields and the `Post` button. Open a saved note from the list; an authenticated admin sees `Delete`.

## Driving it with Playwright

Preconditions: the `just browser` fixture owns the server and seeds its synthetic admin.
Run `just browser`; journey `note validation, Unicode counter, persistence and admin deletion`:

- Log in, enter `Hi 🌍` in `Note`, and observe `4 / 5000`.
- Press `Post` with an empty title; the title is marked invalid, the body remains and the counter still reads `4 / 5000` without a document reload.
- Replace the body with `After swap 🌍`; the counter becomes `12 / 5000`.
- Enter title `Browser regression note` and press `Post`; the note page shows the saved body.
- Reload, choose `All notes`, and reopen that title to prove persistence.
- Press `Delete`; the note disappears from the list and remains absent after reload.

## Gotchas

There is no note-edit feature in the template. Do not invent one for verification.
A successful flash or redirect alone does not prove persistence; the journey reloads real stored data.
