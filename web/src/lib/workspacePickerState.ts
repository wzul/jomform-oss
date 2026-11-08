// Which surface /workspace-picker renders, decided in ONE place.
//
// THE BUG THIS EXISTS FOR (card t_6c0badbe). A new account (zero workspaces)
// submits the ask-first create form with a slug another account already holds.
// The API answers 409 correctly and `createWorkspace` hands the page the
// "That slug is already taken." key — but the page kept ONE `error` state for
// both the LOAD failure and the CREATE failure, and the render branched on it
// before it branched on the list:
//
//     checking ? <loading> : error ? <alert + Retry> : workspaces.length === 0 ? <create form> : <list>
//
// So the create refusal switched the page out of the create form and into the
// generic error panel — and the TITLE branched on the same flag
// (`!error && workspaces.length === 0`), so it fell through to the picker
// heading: "Choose a workspace / You have multiple workspaces. Pick which one
// to enter." That sentence was false (the account had zero workspaces), the
// page listed nothing, and the create fields were gone. Only a reload
// recovered, because a reload re-ran `load()` and cleared the stale error.
//
// THE FIX IS STRUCTURAL, not a copy tweak: mode is a function of the LOAD's
// outcome only. A create failure is not an input to it — `PickerFacts` has no
// field a create refusal could be written into — so the page cannot leave the
// create form because creating failed. The refusal is rendered INSIDE the form,
// which is where the merchant can act on it (fix the name/slug and resubmit).
//
// Kept as a plain .ts module (no JSX) so the repo's Node test runner can import
// and execute it directly — see workspacePickerState.test.ts.

/** The four surfaces the picker can be in. They are mutually exclusive. */
export type PickerMode =
  // The workspace list has not answered yet.
  | "loading"
  // The LIST request failed (network, 5xx, a non-401 refusal). The list is not
  // shown because its contents are unknown — never because it was empty.
  | "loadError"
  // The caller genuinely has zero workspaces: ask-first onboarding, with the
  // create form.
  | "ask"
  // One or more workspaces: render the rows.
  | "list";

/**
 * The facts the mode decision is allowed to see. Deliberately NOT the component
 * state object: a create-refusal field must never be able to reach this
 * decision (that is the defect), so the type is the guard.
 */
export type PickerFacts = {
  checking: boolean;
  loadError: string;
  workspaceCount: number;
};

/**
 * pickerMode answers which surface to render.
 *
 * Order matters and is the fixed part of the contract: a pending load beats
 * everything, a load FAILURE beats an empty list (an empty list you could not
 * read is not an empty account), and only a successfully read empty list asks
 * the user to create a workspace.
 */
export function pickerMode(f: PickerFacts): PickerMode {
  if (f.checking) return "loading";
  if (f.loadError !== "") return "loadError";
  if (f.workspaceCount === 0) return "ask";
  return "list";
}

/**
 * The heading/subtitle pair that belongs to the rendered mode. "ask" has its
 * own first-run copy; every other mode uses the picker's. Kept next to
 * pickerMode so the copy cannot be selected from a different flag than the
 * surface (the pre-fix page selected the heading from `error`, while the body
 * was selected from `error` too — one flag, two meanings).
 *
 * The LIST subtitle is decided by the ROW COUNT, and that count is a required
 * parameter: the picker is reachable with exactly one workspace (no default
 * set), where "You have multiple workspaces. Pick which one to enter." is
 * false — there is nothing to pick. Counting in this function is what keeps
 * the heading and the rows it heads from contradicting each other, and a call
 * site that forgets the count is a compile error rather than a silent fallback
 * to the wrong sentence.
 *
 * While the list is loading or unreadable the count is UNKNOWN, so the neutral
 * line is used: neither the single nor the multiple claim is true.
 */
export function pickerCopyKeys(
  mode: PickerMode,
  workspaceCount: number,
): { title: string; subtitle: string } {
  if (mode === "ask") {
    return { title: "workspace.askPageTitle", subtitle: "workspace.askPageSubtitle" };
  }
  if (mode !== "list") {
    return { title: "workspace.pickerTitle", subtitle: "workspace.pickerNeutralSubtitle" };
  }
  return {
    title: "workspace.pickerTitle",
    subtitle:
      workspaceCount === 1 ? "workspace.pickerSingleSubtitle" : "workspace.pickerSubtitle",
  };
}
