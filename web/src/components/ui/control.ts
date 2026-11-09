// The control surface shared by Input, Textarea, Select and CodeInput (card
// t_f2e231d3; CodeInput split by card t_8f20418d).
//
// WHY THIS IS ONE CONSTANT AND NOT THREE COPIES: a border colour, a padding and a
// focus treatment written three times is how the app ended up with two border
// colours and three focus treatments across 61 pages. The primitives differ
// only in the ELEMENT they render; everything about how a control looks is here.
//
// WHY THE TYPE STEP IS *NOT* IN `CONTROL_SURFACE` (card t_8f20418d). A call site
// cannot override it: `text-jf-read-sm` is emitted AFTER `text-jf-read-2xl` in the
// built stylesheet (measured in `.next/static/chunks/*.css`: read-2xl at byte
// 44167, read-sm at 44637), and two utilities of equal specificity are decided by
// source order, not by the order of the class attribute. So `className="text-jf-read-2xl"`
// on an `Input` renders at 14px and nothing warns. A control whose type step is part
// of its meaning (a 6-digit one-time code) therefore gets its own primitive that
// starts from `CONTROL_SURFACE` and states its own step — rather than a page
// fighting the base class with an override that cannot win.
//
// `jf-control` is a real class in `app/globals.css` (and it is the one piece that
// cannot be a utility): the global `:focus-visible` rule sets `border-radius: 4px`,
// and it is unlayered, so it outranks any Tailwind radius utility on the same
// element. `.jf-control:focus-visible` re-declares the radius at the same
// specificity from a block that sits below it, so a focused 8px control keeps an
// 8px ring instead of snapping to 4px.

export const CONTROL_SURFACE =
  "jf-control w-full rounded-jf-md border border-[var(--jf-border-strong)] " +
  "bg-[var(--jf-surface)] px-jf-3 py-jf-2 " +
  "text-[var(--jf-text)] " +
  "placeholder:text-[var(--jf-muted)] transition-colors " +
  "focus:border-[var(--jf-primary)] " +
  "disabled:cursor-not-allowed disabled:opacity-60 " +
  "aria-invalid:border-[var(--jf-danger-text)]";

export const CONTROL_CLASS = `${CONTROL_SURFACE} text-jf-read-sm`;
