// Cross-cutting htmx behavior, bound once on document.

/** Cross-fade boosted navigations via the browser's View Transition API. */
export function enableViewTransitions(): void {
  htmx.config.transitions = true;
}
