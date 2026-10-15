// charCount — how much of a text field's limit is used, for the form of a new note. Alpine owns one piece of UI
// state: the count of the characters typed. It is bound once on the wrapper of the field, since input events bubble
// up from the textarea, and reads the limit from data-max, which the server writes from the same constant it
// checks against (note.MaxBody).
//
// This only tells the visitor. The server counts again and refuses a note that is too long.

/** The number of characters of s as the server counts them: code points, not UTF-16 units. */
export function countChars(s: string): number {
  return [...s].length;
}

/** The state of the component, with the element Alpine binds it to ($el). */
type CharCount = { count: number; max: number; $el: HTMLElement };

export function registerCharCount(): void {
  Alpine.data("charCount", () => ({
    count: 0,
    max: 0,
    init(this: CharCount) {
      const el = this.$el;
      this.max = Number(el.dataset.max ?? 0);
      this.count = countChars(el.querySelector("textarea")?.value ?? "");
    },
    onInput(this: CharCount, e: Event) {
      const field = e.target as HTMLTextAreaElement;
      if (field.tagName !== "TEXTAREA") return;
      this.count = countChars(field.value);
    },
  }));
}
