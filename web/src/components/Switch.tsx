"use client";

// The app's ONE switch control (card t_b73eee21).
//
// WHY THIS EXISTS AS A COMPONENT AND NOT THREE COPIES OF THE MARKUP
//   The same hand-rolled pill was written out in three files — settings
//   Storefront (4 switches), form-settings (1) and /sales (1) — and each copy
//   had to get the same four things right on its own: the pointer cursor, the
//   OFF colour, the knob's edge and a hover cue. Every copy got all four wrong
//   in the same way: the built page measured `cursor: default` on every one of
//   them, `bg-slate-300` as the OFF track (a white knob on it is 1.48:1, so OFF
//   read as a disabled chip on a white card) and no hover state at all. Fixing
//   five call sites by hand leaves the sixth to repeat it, so the markup and the
//   states live here and in `.jf-switch*` (`app/globals.css`).
//
// WHAT IS DELIBERATELY *NOT* HERE
//   No state, no fetching, no debouncing. This is the presentation of a switch
//   and nothing else — the caller owns the boolean and the write:

//     <Switch checked={sfIndexing} onChange={() => setSfIndexing((v) => !v)}
//       label={t("set.sfIndexingTitle")} />
//
//   The previous markup's `onClick={() => setX((v) => !v)}` semantics, the
//   `aria-checked` value and the wire behaviour are unchanged by this component.
//
// ACCESSIBILITY
//   `role="switch"` + `aria-checked` are on the button, exactly as before, and
//   `type="button"` is pinned so the control cannot submit a surrounding form
//   (the old markup set it on every copy except /sales, which renders the switch
//   outside a form — this removes the difference). `label` becomes `aria-label`,
//   which is what the /sales copy already passed and the settings copies already
//   needed: three of the four settings switches previously had NO accessible
//   name derived from their own heading, because the heading is a sibling `<p>`
//   and not a `<label>`.
//
// TOOLTIP (`title`)
//   `label` is the ACCESSIBLE NAME, which a sighted mouse user does not see. The
//   hand-rolled /sales pill also carried a `title` — the hover tooltip — and in
//   the conversion to this component it was dropped (card t_5ca04dc0, measured
//   on the built page: `hasTitleAttr` true before, false after). The tooltip is
//   the only place that copy was visible: before the workspace's stored value is
//   read, /sales passed `t("sales.soundLoading")` so a mouse user hovering the
//   DISABLED switch is told it is still loading. `title` is therefore forwarded
//   untouched. It is optional and defaults to nothing: a switch whose heading is
//   a visible sibling does not need one.

export function Switch({
  checked,
  onChange,
  label,
  title,
  disabled = false,
}: {
  checked: boolean;
  onChange: () => void;
  /** Accessible name. Pass the same string the switch's visible heading uses. */
  label: string;
  /** Hover tooltip. Optional: `label` is announced, this is what is SEEN. */
  title?: string;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      title={title}
      disabled={disabled}
      onClick={onChange}
      className="jf-switch"
    >
      {/* Decoration: the checked state is carried by `aria-checked` on the
          button, so the knob must not be announced a second time. */}
      <span aria-hidden="true" className="jf-switch-knob" />
    </button>
  );
}
