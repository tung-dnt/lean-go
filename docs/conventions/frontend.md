# Frontend

Frontend code lives in `web/js`; the area map and build are in [LEANGO.md](../../LEANGO.md#tests-and-browser-areas). JavaScript is organized by area and Alpine component, not by a second global app framework.
Run JavaScript tooling with Bun as the runtime, not merely as the package manager. **Do:** use explicit Bun runtime selection for package CLIs (`bunx --bun`, `bun x --bun`) and in package scripts. **Don't:** invoke a package binary bare (`tsc`, `npx`, or `bun x` without `--bun`), because its Node shebang can select Node.

**Do:** put a component in its area folder (for example `web/js/note/charCount.ts`), register it in `web/js/register.ts`, and name it in markup with `x-data`. Treat each `data-*` attribute read by the component as an interface with the server-rendered view; document that contract on both sides. If adding an area or an edge between areas, update the `webAreas` allowlist in the architecture test and the area map in [LEANGO.md](../../LEANGO.md#tests-and-browser-areas).

```ts
// web/js/register.ts
import { registerCharCount } from "./note/charCount";

export function registerComponents(): void {
  document.addEventListener("alpine:init", () => {
    registerCharCount();
  });
}
```
**Don't:** reach across areas through an undeclared import or add a component without registering it and matching its markup contract. Keep behavior server-rendered where possible; a component should own only the interaction that needs client-side behavior.

## htmx 4 and Alpine 3

The bundle uses htmx **4.0.0**, Alpine.js **3.17.5** and `@types/alpinejs` **3.17.0**.
Versions are pinned in `package.json` and `bun.lock`; `just js` embeds the bundle, with no CDN.

**Do:** declare inheritance explicitly. `components.HxBoost()` renders `hx-boost:inherited="true"`
for forms and navigation parents. A parent swap/preload policy also needs `:inherited`:

```html
<nav hx-boost:inherited="true"
     hx-swap:inherited="innerHTML show:window:top"
     hx-preload:inherited="mouseover">
  <a href="/">Notes</a>
</nav>
```

Load `htmx.org/dist/ext/hx-preload.js` from the same htmx package, after core initialization.
The app disables `htmx.config.preload.autoBoost` and explicitly opts safe navigation into preload.
Never enable speculative requests for logout or another mutating GET. The template's account
menu remains excluded; adding a new mutation must use POST rather than copying GET logout.

**Don't:** use `hx-ext`, bare inherited `hx-boost`/`hx-swap`, the v2 preload package,
`globalViewTransitions` or XHR-shaped events. Use `htmx.config.transitions` for view transitions.
htmx 4 uses fetch and swaps error responses natively; do not reintroduce an error-swap listener.
`layouts.Error` already returns a full page for boosted requests and a fragment for other htmx requests.
Use v4 event names such as `htmx:after:swap` only when a real interaction requires a listener.

Register components with `Alpine.data()` during `alpine:init`, before the single `Alpine.start()`.
Alpine invokes `init()` automatically; do not add `x-init="init()"` or call start again after a swap.
Use `$root` when a directive needs the component root; `$el` is the element evaluating that directive.
Use `x-transition`, `x-bind` and `.outside`, not `x-show.transition`, `x-spread` or `.away`.
The shared document declares `<body x-data>` as a data-less root. The single `Alpine.start()`
initializes that root and its descendants; the observer initializes added content and destroys
removed components. Keep resources in `init()`/`destroy()`; do not manually call `initTree`,
`destroyTree` or restart Alpine after htmx swaps. Nested components retain their own local state.

**Verify:** boosted navigation without a full reload, form validation and redirect, back/forward
re-fetch, rendered 4xx/5xx responses, safe hover preload, no speculative logout, and Alpine state
after repeated swaps. A TypeScript check or successful bundle alone does not prove these work.

References: [htmx guide and migration checker](https://four.htmx.org/docs),
[htmx 4 changes](https://four.htmx.org/docs/whats-new-in-htmx-4),
[preload](https://four.htmx.org/extensions/hx-preload),
[Alpine upgrade guide](https://alpinejs.dev/upgrade-guide),
[Alpine releases](https://github.com/alpinejs/alpine/releases).
