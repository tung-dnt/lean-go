// Cross-cutting htmx behavior, bound once on document.

/** Cross-fade boosted navigations via the browser's View Transition API. */
export function enableViewTransitions(): void {
  htmx.config.globalViewTransitions = true;
}

/**
 * Swap error responses (>= 400) into <body> instead of dropping them, so the
 * server-rendered error page is shown rather than a silent no-op.
 */
export function initErrorSwap(): void {
  document.body.addEventListener("htmx:beforeSwap", (evt) => {
    const detail = (evt as CustomEvent).detail as {
      xhr: XMLHttpRequest;
      shouldSwap: boolean;
      target: Element | null;
    };
    if (detail.xhr.status >= 400) {
      detail.shouldSwap = true;
      detail.target = htmx.find("body");
    }
  });
}
