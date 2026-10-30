// Entry point for all UI JavaScript. Bundled by esbuild into
// public/static/js/app.js (see `just js`) and loaded once in <head>.
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
// Core sets window.htmx before this bundled v4 extension registers.
import "htmx.org/dist/ext/hx-preload.js";

import { registerComponents } from "./register";
import { enableViewTransitions } from "./shell/htmx";

// Shared runtime globals for Alpine expressions, extensions and shell modules.
window.htmx = htmx;
window.Alpine = Alpine;

// Configure before htmx initializes the parsed DOM. Preload is opt-in: the
// account menu contains GET /logout, which must never run speculatively.
Object.assign(htmx.config, { preload: { autoBoost: false } });
enableViewTransitions();

// Register Alpine components before Alpine.start() dispatches `alpine:init`.
registerComponents();

// Alpine's npm build does not auto-start (unlike the CDN build) — start it
// explicitly. It scans the already-parsed DOM and its observer handles later
// hx-boost swaps.
Alpine.start();
