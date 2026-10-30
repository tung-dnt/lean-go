# Frontend

Frontend code lives in `web/js`; the area map and build are in [LEANGO.md](../../LEANGO.md#tests-and-browser-areas). JavaScript is organized by area and Alpine component, not by a second global app framework.

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
