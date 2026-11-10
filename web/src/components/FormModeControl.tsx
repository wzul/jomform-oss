"use client";

// Card t_6dbc1e16 — the form's COLLECTION MODE lives in the builder HEADER,
// not inside the field-settings panel.
//
// WHY IT MOVED (measured, not a preference):
//   * The aside is titled "Block settings" and renders one selected block's
//     form. The mode card and the "Form settings →" card used to be drawn
//     UNCONDITIONALLY at its bottom, under that label — two scopes, one heading,
//     no divider. The builder's own comment next to them said "the builder is
//     fields-only", which the panel contradicted.
//   * On a phone the panel is a full-screen scroll (~1,450px tall with a choice
//     field selected), so the form-level control sat at the bottom of a field
//     editor — the discoverability argument for putting it there does not
//     survive the mobile layout.
//   * Switching Payment → Response has two side effects a merchant cannot see
//     from two buttons: an untouched default notification is re-pointed on the
//     next save (api/internal/dbstore/form_notification.go, ReconcileDefault),
//     and payment blocks already placed (price, quantity, add-on, shipping,
//     total) are not drawn by the response page at all
//     (api/internal/httpapi/render_blocks.go, renderResponseForm — the case is
//     written out: "the builder filters the pricing group out of the response
//     palette, but does not remove blocks already placed"). A silent re-point
//     one scroll below the "Required" checkbox is the defect this dialog closes.
//
// WHAT THIS RENDERS: a compact status button in the header reading the CURRENT
// mode ("Payment form" / "Response form"), always visible without selecting a
// block. It opens a dialog holding the two choices, their hints, the "?" doc
// link (bld.modeDocs → /docs/email#notifications), and — when the pending
// choice differs from the current one — the consequences above. Nothing is
// applied until Confirm: the caller's `onApply` runs only from that button.
//
// The dialog is PORTALLED to <body> on purpose: the builder header carries
// `backdrop-blur`, and a backdrop-filter ancestor becomes the containing block
// for `position: fixed` descendants, so an in-place overlay would be positioned
// against the header instead of the viewport.

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "./i18n";

export type FormMode = "payment" | "response";

const MODES: FormMode[] = ["payment", "response"];

// The i18n KEY is written as a literal ternary at each `t()` call site, not
// through a helper: src/lib/i18nKeys.ts resolves keys statically and treats a
// helper call as unresolvable, and its ceiling fails on any new unreadable site.
//
// The event each mode's DEFAULT notification fires (api/internal/dbstore:
// NotifyEventSalePaid / NotifyEventResponseSubmitted). Shown to the merchant
// because re-pointing is the consequence they cannot see.
const eventFor = (m: FormMode) => (m === "response" ? "response.submitted" : "sale.paid");

export default function FormModeControl({
  mode,
  saved,
  pricingBlockCount,
  onApply,
}: {
  /** The mode the form is in right now. */
  mode: FormMode;
  /** True when the form already exists: a saved form has a default notification
   *  row that the next config write reconciles, so the re-point applies to it.
   *  A brand-new form has no row yet — it is created on the mode it is born
   *  with, so there is nothing to re-point and the note is not shown. */
  saved: boolean;
  /** Payment blocks already placed (price, quantity, add-on, shipping, total).
   *  A response form never draws them, so switching with any of them present
   *  silently stops collecting money for them. */
  pricingBlockCount: number;
  onApply: (m: FormMode) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [mounted, setMounted] = useState(false);
  const [pending, setPending] = useState<FormMode>(mode);
  const dialogRef = useRef<HTMLDivElement | null>(null);
  const currentLabel = t(mode === "response" ? "bld.modeResponse" : "bld.modePayment");

  // Portal target exists only in the browser; the dialog can only open from a
  // click, so this guard is belt-and-braces for the first hydration render.
  useEffect(() => setMounted(true), []);

  // Move focus to the option that is already chosen. The dialog opens from a
  // button in the header, so without this the merchant lands at the top of the
  // document and has to tab back into it.
  useEffect(() => {
    if (!open) return;
    const chosen = dialogRef.current?.querySelector<HTMLElement>("[aria-pressed='true']");
    chosen?.focus();
  }, [open]);

  function openDialog() {
    setPending(mode);
    setOpen(true);
  }

  function apply() {
    if (pending !== mode) onApply(pending);
    setOpen(false);
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Escape") {
      e.stopPropagation();
      setOpen(false);
      return;
    }
    if (e.key !== "Tab") return;
    // Focus stays inside the dialog while it is open (same trap the docs search
    // dialog uses): the builder behind it is still tabbable otherwise.
    const focusable = dialogRef.current?.querySelectorAll<HTMLElement>("button, a[href]");
    if (!focusable || focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }

  const changed = pending !== mode;

  return (
    <>
      <button
        type="button"
        onClick={openDialog}
        aria-haspopup="dialog"
        aria-expanded={open}
        title={t("bld.mode")}
        className="shrink-0 rounded-md border border-[var(--jf-border)] bg-white px-2 py-1 text-xs font-medium text-[var(--jf-text-secondary)] hover:bg-slate-50"
      >
        {/* The visible text IS the current mode; the sr-only prefix keeps the
            accessible name unambiguous ("Form mode: Payment form") without
            hiding the label the merchant reads. */}
        <span className="sr-only">{t("bld.mode")}: </span>
        {currentLabel}
        <span aria-hidden="true" className="ml-1 text-[var(--jf-muted)]">
          ▾
        </span>
      </button>

      {mounted &&
        open &&
        createPortal(
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
            onMouseDown={(e) => {
              if (e.target === e.currentTarget) setOpen(false);
            }}
          >
            <div
              ref={dialogRef}
              role="dialog"
              aria-modal="true"
              aria-labelledby="jf-form-mode-title"
              onKeyDown={onKeyDown}
              className="jf-enter jf-card max-h-[85vh] w-full max-w-md overflow-y-auto p-4"
            >
              <div className="flex items-start justify-between gap-2">
                <h2 id="jf-form-mode-title" className="text-base font-semibold">
                  {t("bld.mode")}
                </h2>
                {/* AGENTS.md DOCUMENTATION PARITY rule 4: the inline "?" next to
                    the feature it documents. The mode decides which default
                    notification the form gets and switching re-points an
                    untouched default, which is not visible from two buttons —
                    so the guide that explains it is one link away (issue #924:
                    title + aria-label + new tab). */}
                <a
                  href="/docs/email#notifications"
                  target="_blank"
                  rel="noreferrer"
                  title={t("bld.modeDocs")}
                  aria-label={t("bld.modeDocs")}
                  className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full border border-[var(--jf-border)] text-jf-2xs font-semibold text-[var(--jf-muted)] hover:bg-slate-50 hover:text-slate-700"
                >
                  ?
                </a>
              </div>

              <div className="mt-3 grid gap-2">
                {MODES.map((m) => (
                  <button
                    key={m}
                    type="button"
                    aria-pressed={pending === m}
                    onClick={() => setPending(m)}
                    className={`rounded-md border px-3 py-2 text-left transition-colors ${
                      pending === m
                        ? "border-[var(--jf-accent)]/60 bg-indigo-50"
                        : "border-[var(--jf-border)] hover:bg-slate-50"
                    }`}
                  >
                    <span
                      className={`block text-sm font-medium ${
                        pending === m
                          ? "text-[var(--jf-primary-on-tint)]"
                          : "text-[var(--jf-text-secondary)]"
                      }`}
                    >
                      {t(m === "response" ? "bld.modeResponse" : "bld.modePayment")}
                    </span>
                    {/* Per the guard's own rule (src/lib/tintedSurface.test.ts):
                        on a CONDITIONAL tint use the on-tint token
                        unconditionally — it also passes on white (5.98:1), so
                        neither branch is the base token on a tint. */}
                    <span className="mt-0.5 block text-jf-2xs text-[var(--jf-muted-on-tint)]">
                      {t(m === "response" ? "bld.modeResponseHint" : "bld.modePaymentHint")}
                    </span>
                  </button>
                ))}
              </div>

              {/* The consequences, shown only for a choice that would CHANGE the
                  form. Each is a fact the merchant cannot see from here. */}
              {changed && ((pending === "response" && pricingBlockCount > 0) || saved) && (
                <div className="mt-3 space-y-1.5">
                  {pending === "response" && pricingBlockCount > 0 && (
                    <p
                      role="alert"
                      className="rounded-md border border-[var(--jf-warning-text)] bg-[var(--jf-warning-bg)] p-2 text-jf-2xs leading-relaxed text-[var(--jf-warning-text)]"
                    >
                      {t("bld.modePricingNote", { n: pricingBlockCount })}
                    </p>
                  )}
                  {saved && (
                    <p className="rounded-md border border-[var(--jf-border)] bg-slate-50 p-2 text-jf-2xs leading-relaxed text-[var(--jf-text-secondary)]">
                      {t("bld.modeNotifyNote", { event: eventFor(pending) })}
                    </p>
                  )}
                </div>
              )}

              <div className="mt-4 flex justify-end gap-2">
                <button type="button" onClick={() => setOpen(false)} className="jf-btn-ghost">
                  {t("common.cancel")}
                </button>
                <button
                  type="button"
                  onClick={apply}
                  disabled={!changed}
                  className="jf-btn-primary disabled:opacity-50"
                >
                  {t("common.confirm")}
                </button>
              </div>
            </div>
          </div>,
          document.body,
        )}
    </>
  );
}
