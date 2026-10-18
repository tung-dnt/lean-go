// Registry for reusable Alpine components and stores. Each lives in its own file
// in an area folder (shell/, note/), exporting a register function; wire it up
// here with an import plus a call inside the single alpine:init listener.
// Reference a registered component by name in gomponents markup, e.g.
// Div(Attr("x-data", "charCount"), ...). Alpine's MutationObserver initializes
// them automatically, including on hx-boost swaps, so no manual re-init is
// needed after navigation.
//
// Split by concern: a component file registers with Alpine and holds UI state;
// every other file of an area is framework-free data/platform logic. The areas
// import each other only along the edges that internal/architecture_test.go
// holds them to (LEANGO.md, Architecture), and this file, which imports every
// area, is imported by app.ts alone. For heavy imperative widgets (charts,
// canvas, third-party libs), give the component init()/destroy() so setup and
// teardown are lifecycle-managed across swaps.
//
// Code splitting: components register eagerly (they are tiny), but any npm
// package or framework-free module only some pages need (IndexedDB, charts,
// croppers) must be loaded with import() from the component that uses it, so
// esbuild emits it as a separate js/chunks/ file. Never import such a package
// statically anywhere, or it lands back in app.js. Start the load in init()
// (and clean up in destroy()); for user-gesture APIs it must be loaded before
// the tap.

import { registerCharCount } from "./note/charCount";

export function registerComponents(): void {
  document.addEventListener("alpine:init", () => {
    registerCharCount();
    // Add future components here — one import above, one call below.
  });
}
