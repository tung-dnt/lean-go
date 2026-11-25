# Authentication

Visitors can log in with the seeded account, see credential errors, and log out without a speculative hover request ending their session.

## Sub-features

- Invalid credentials preserve email and clear password.
- Valid login survives reload; the welcome flash is consumed once.
- Hovering Log out does not log out; clicking it does.
- Back/forward after logout does not restore authenticated navigation.

## How to get to it (user POV)

Open `/` and choose `Log in` in the sidebar. The authenticated sidebar contains `Log out`.

## Driving it with Playwright

Preconditions: the `just browser` fixture owns the server and seeds its synthetic admin.
Run `just browser`; journey `login errors, authentication, hover-safe logout and history`:

- Choose `Log in`; the `#login` form appears without a document reload.
- Fill `Email address` and an incorrect `Password`, then press `Login`; the alert says `Invalid credentials`, email remains, password is blank.
- Enter the fixture password and press `Login`; `Notes`, the welcome alert and `Log out` appear.
- Hover `Log out`; no `/logout` request arrives during the observation window. Reload; the session remains, the welcome alert is gone.
- Click `Log out`; `Log in` appears and `Log out` disappears. Go back to the login form, then forward to the anonymous note list; authenticated navigation stays absent.

## Gotchas

The credentials are synthetic and passed by the Go fixture, not copied from development config.
Logout changes server session state through a GET route, so the hover check is essential.
