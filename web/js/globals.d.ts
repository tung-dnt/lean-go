// htmx and Alpine are bundled from npm (see package.json) and exposed as globals
// in app.ts for parity with the old CDN builds. These declarations give the
// editor the official types for both the bare globals (used by the area modules)
// and the window.* assignments. `import type` is erased before esbuild bundles.

import type htmx from "htmx.org";
import type Alpine from "alpinejs";

declare global {
  const htmx: typeof htmx;
  const Alpine: typeof Alpine;

  interface Window {
    htmx: typeof htmx;
    Alpine: typeof Alpine;
  }
}

export {};
