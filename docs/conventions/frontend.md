# Frontend

The TypeScript in `web/js`, its areas and its Alpine components. The area table and the build are in [LEANGO.md](../../LEANGO.md#frontend).

- **Frontend.** A component is an Alpine component in an area folder of `web/js` (`note/charCount.ts`), registered in `web/js/register.ts` and named in the markup with `x-data`. The `data-*` attributes it reads are its contract with the view: say so in a comment on both sides. A new area, or an edge between areas, is a row in `webAreas` of the architecture test and in the area table of [LEANGO.md](../../LEANGO.md#frontend).
