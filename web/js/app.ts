// Entry point for all UI JavaScript. Bundled by esbuild into
// public/static/js/app.js (see `make js`) and loaded once in <head>.
//
// htmx and Alpine are bundled in here from npm (see package.json) rather than
// loaded from a CDN, so package.json is the single source of truth for their
// versions and the app ships fully self-contained (no third-party runtime deps).
//
// Everything here binds to `document` or registers with htmx/Alpine, so it stays
// active across hx-boost navigations without re-running per page. To add page-level
// interactivity, prefer an Alpine component, registered in ./register — not a per-page script.

import Alpine from "alpinejs";
import htmx from "htmx.org";
// Registers the `preload` extension on the shared htmx singleton (side-effect
// import; the ext imports htmx itself, so no global/ordering shim needed).
// Enabled per-subtree via hx-ext="preload" (see the boosted nav in primary.go).
import "htmx-ext-preload";

import { registerComponents } from "./register";
import { enableViewTransitions, initErrorSwap } from "./shell/htmx";

// Expose the globals the CDN builds used to create, for parity (inline hx-on
// handlers, Alpine plugins, browser devtools) and for the shell modules that
// reference the `htmx` global.
window.htmx = htmx;
window.Alpine = Alpine;

// htmx self-initializes on import; globalViewTransitions is read per-swap, so
// setting it now (before any navigation) is in time.
enableViewTransitions();

// Register Alpine components before Alpine.start() dispatches `alpine:init`.
registerComponents();

function boot(): void {
  initErrorSwap();
}

// This script is deferred, so parsing is usually done — but guard for safety.
if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", boot);
} else {
  boot();
}

// Alpine's npm build does not auto-start (unlike the CDN build) — start it
// explicitly. It scans the already-parsed DOM and its observer handles later
// hx-boost swaps.
Alpine.start();

// start() initializes only the components of the parsed page, not the elements around them, and Alpine's observer
// ignores the removal of an element it has not initialized. So when hx-boost swaps out the page the browser
// loaded, which removes the wrappers of its components and not the components themselves, none of them is ever
// destroyed: a destroy() that clears a timer does not run for that page (pages swapped in
// later are initialized whole, and are fine). Walking the document once marks the wrappers too. Elements already
// initialized are skipped, so nothing runs twice.
Alpine.initTree(document.body);
