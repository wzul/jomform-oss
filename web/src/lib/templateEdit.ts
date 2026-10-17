// Builder edit target: which record the builder is editing, and how it may be
// saved.
//
// The Templates page lists the merchant's own templates (issue #457) and now
// opens one in the builder with ?template=<id>. The builder already edited
// FORMS (?form=<id>). Both are `forms` rows — a template is a row with
// is_template=true and status='draft' — but they must NOT share a save path:
//
//   - a FORM      (?form=<id>)     ->  GET  /api/rest/forms/{id}
//                                      PATCH /api/rest/forms/{id}
//                                      POST /api/rest/forms/{id}/status
//                                      (+ POST /forms/{id}/publish on Publish)
//   - a TEMPLATE  (?template=<id>) ->  GET  /api/rest/templates/{id}
//                                      PUT  /api/rest/templates/{id}
//
// Publishing a template sets status='published', which breaks TWO things at
// once, because the two template queries are disjoint by status:
//
//   * ListTemplatesByAccount (the /templates page) is
//     `account_id=$1 AND is_template AND NOT (status='published')`, so the row
//     vanishes from the merchant's own list;
//   * ListTemplates (the public gallery) is
//     `is_template AND status='published'` with NO account filter, so the row
//     appears in EVERY merchant's New-form picker.
//
// So the template contract never writes a status and never publishes — not even
// when the caller asks to publish. Everything here is pure so the decision is
// testable without a component renderer (this repo has no JSX test setup — see
// src/lib/formMode.test.ts for the same pattern), and the builder executes the
// plan returned by `savePlan` rather than re-deciding inline.
//
// A template id also wins over a form id when both are present: silently
// treating a template as a form is exactly the failure this guards.

export type BuilderTarget =
  | { kind: "template"; id: string }
  | { kind: "form"; id: string }
  | { kind: "new" };

/** Resolves the builder's edit target from the URL query parameters. */
export function builderTarget(params: {
  template?: string | null;
  form?: string | null;
  isNew?: boolean;
}): BuilderTarget {
  const template = (params.template || "").trim();
  if (template) return { kind: "template", id: template };
  const form = (params.form || "").trim();
  if (form) return { kind: "form", id: form };
  return { kind: "new" };
}

/** Where the builder reads the target from. */
export function loadPath(target: BuilderTarget): (id: string) => string {
  if (target.kind === "template") return (id) => `/api/rest/templates/${id}`;
  return (id) => `/api/rest/forms/${id}`;
}

/** One write the builder performs when saving. */
export type SaveStep =
  // Create or update the record itself (the fields + design).
  | "writeRecord"
  // POST /forms/{id}/status — a FORM-only route.
  | "writeStatus"
  // POST /forms/{id}/publish — a FORM-only route.
  | "publish";

export type SavePlan = {
  steps: SaveStep[];
  /** HTTP method for `writeRecord`. */
  method: "POST" | "PUT" | "PATCH";
  /** Path builder for `writeRecord`; pass the current record id. */
  recordPath: (id: string) => string;
  /** True when this plan can publish the record into the gallery. */
  canPublish: boolean;
};

/**
 * The writes the builder performs for a target. Mirrors the existing form flow
 * exactly — a NEW form is POSTed and then (once it has an id) has its status
 * written like any other — so template mode is the only branch that differs.
 *
 * A template saves through PUT /api/rest/templates/{id} and NOTHING else: no
 * status write, no publish, even when `publish` is true. That is the whole
 * point of the plan — a caller cannot accidentally publish a template into the
 * public gallery, because there is no step for it.
 */
export function savePlan(target: BuilderTarget, publish: boolean): SavePlan {
  if (target.kind === "template") {
    return {
      steps: ["writeRecord"],
      method: "PUT",
      recordPath: (id) => `/api/rest/templates/${id}`,
      canPublish: false,
    };
  }
  const steps: SaveStep[] = ["writeRecord", "writeStatus"];
  if (publish) steps.push("publish");
  return {
    steps,
    method: target.kind === "new" ? "POST" : "PATCH",
    recordPath: (id) => `/api/rest/forms/${id}`,
    canPublish: true,
  };
}

/** The PUT body for a template save. `theme` is omitted when unknown so the
 *  API's COALESCE keeps the stored theme instead of clearing it. */
export function templateSaveBody(
  title: string,
  config: unknown,
  theme?: Record<string, unknown> | null,
): { title: string; config: unknown; theme?: Record<string, unknown> } {
  const body: { title: string; config: unknown; theme?: Record<string, unknown> } = { title, config };
  if (theme && typeof theme === "object") body.theme = theme;
  return body;
}

/**
 * The config a template save writes: the builder's freshly-built config laid
 * over the stored one.
 *
 * The builder owns the keys it renders (`mode`, `blocks`, `products`,
 * `description`, `chip_mapping`), and those must win — that is the edit. But a
 * stored template config can legitimately carry a key the builder has no
 * control for, and the gallery's own metadata is exactly that:
 * `config.category` is what a gallery card shows. A wholesale replace would
 * silently strip it and the card would lose its category, so the stored keys
 * survive unless the builder explicitly owns them.
 */
export function templateConfigWithStoredKeys(
  storedConfig: Record<string, unknown> | null | undefined,
  builtConfig: Record<string, unknown>,
): Record<string, unknown> {
  if (!storedConfig || typeof storedConfig !== "object") return builtConfig;
  // ...stored first, built second: builder-owned keys win, unknown stored keys
  // are carried through. A builder key set to undefined stays undefined and is
  // dropped by JSON.stringify, which is how a cleared field clears.
  return { ...storedConfig, ...builtConfig };
}

// Which builder controls are legitimate for each target. Everything that would
// write a status, publish, or reach the form-only settings surface is off in
// template mode; the field/design controls (palette, inspector, form mode, AI
// drafting) stay on, because they only change the record's own config.
export const TEMPLATE_BUILDER_CONTROLS = {
  slug: false,
  publish: false,
  saveAsTemplate: false,
  formSettings: false,
  view: false,
  aiGenerate: true,
  mode: true,
} as const;

export const FORM_BUILDER_CONTROLS = {
  slug: true,
  publish: true,
  saveAsTemplate: true,
  formSettings: true,
  view: true,
  aiGenerate: true,
  mode: true,
} as const;

export function builderControls(kind: "template" | "form" | "new") {
  return kind === "template" ? TEMPLATE_BUILDER_CONTROLS : FORM_BUILDER_CONTROLS;
}

/**
 * The builder's identity AFTER it creates a form (t_b3cc2a7a).
 *
 * The create path POSTs to the collection and the API answers with the new row.
 * From that moment the session is EDITING that row, and the builder must say so
 * in its own state AND in its URL. Both halves were missing, and each produced
 * its own symptom:
 *
 *   * the target stayed `{kind:"new"}`, so `savePlan` kept answering POST — and
 *     the second Save of a new-form session therefore sent
 *     `POST /api/rest/forms/{id}`. There is no such route (only
 *     `POST /forms` and `PATCH /forms/{id}`), so the API answered 405, which
 *     carries no machine `code`, so the builder rendered its generic
 *     "Failed to save." with no cause named.
 *   * the URL was never rewritten, so it stayed `/builder?new=1` (or bare).
 *     A refresh re-ran `builderTarget` on that URL, which reads `?form=` and
 *     `?template=` only — so it started a BRAND-NEW empty form instead of
 *     reopening the one just created and published.
 *
 * Returns `null` for an id the builder cannot identify (a response body with no
 * id): adopting an empty id would make the next save PATCH `/forms/undefined`
 * and write `?form=undefined` into the URL, which a refresh turns into a 404.
 * Callers keep the pre-existing behaviour in that case rather than inventing an
 * identity.
 */
export function adoptedTarget(id: string | null | undefined): BuilderTarget | null {
  const created = (id || "").trim();
  if (!created) return null;
  // A create can only come from the new-form path: template mode refuses to
  // create anything when its own row failed to load (see save() in
  // app/builder/page.tsx), so the adopted identity is always a form.
  return { kind: "form", id: created };
}

/**
 * The URL that CARRIES a builder identity, so a reload/reopen comes back to the
 * same record. This is the same string shape `builderTarget` parses, which is
 * what makes the round trip (adopt -> replaceState -> refresh -> same record)
 * testable without a component renderer.
 */
export function builderHref(target: BuilderTarget): string {
  if (target.kind === "template") return `/builder?template=${encodeURIComponent(target.id)}`;
  if (target.kind === "form") return `/builder?form=${encodeURIComponent(target.id)}`;
  return "/builder?new=1";
}

/** The Templates-page link that opens one of the merchant's own templates. */
export function templateEditHref(id: string): string {
  return builderHref({ kind: "template", id });
}

/**
 * The id "Save as template" snapshots, and the save it may need first
 * (t_c33effe4).
 *
 * A form has to exist before it can be snapshotted, so the button saves when the
 * session has no id yet. That save WRITES the id — but through `setCurrentId`,
 * and `currentId` is a value captured by the RENDER's closure, not a live
 * binding: the re-render `setCurrentId` queues cannot rebind a handler that is
 * already awaiting. The old code awaited the save and then re-read `currentId`,
 * so it read its own stale `null` and returned. On an UNSAVED form that meant
 * the row WAS created (POST /api/rest/forms 201 + status 200) while the template
 * was never posted and no message was shown at all — the early return skipped
 * both `setTplMsg` arms, so the merchant got neither "Saved as a template." nor
 * "Failed to save as template.".
 *
 * So the id a save WROTE is taken as a value and never re-read from state.
 * `ensureSaved` is called ONLY when there is no id yet: an already-saved form
 * must not be saved again, which is a write the merchant did not ask for.
 */
async function templateSnapshotId(
  currentId: string | null,
  ensureSaved: () => Promise<string | null>,
): Promise<string | null> {
  if (currentId) return currentId;
  return (await ensureSaved()) || null;
}

/** What "Save as template" must report back to the merchant. */
export type TemplateSaveOutcome = "saved" | "failed" | "skipped";

/**
 * The whole decision behind the builder's "Save as template" button, so the
 * three things it must never do are testable without a component renderer:
 *
 *   - never create a template for a form the save did NOT write ("skipped" — a
 *     failed save leaves its own error on screen, and an error screen must not
 *     become a template. That is what the old `if (!currentId) return` was for,
 *     and it is kept);
 *   - never snapshot a STALE id: the id is the save's return value, never a
 *     re-read of the `currentId` this render closed over (t_c33effe4);
 *   - never fail in silence: every finished attempt answers "saved" or "failed",
 *     so the caller always has a sentence to render. A save that throws is one
 *     of those ("failed") — it wrote no id and rendered nothing itself.
 *
 * `snapshot` performs the template POST and answers whether it was accepted; it
 * may throw (a network drop), which is also a "failed".
 */
export async function saveFormAsTemplate(deps: {
  currentId: string | null;
  ensureSaved: () => Promise<string | null>;
  snapshot: (id: string) => Promise<boolean>;
}): Promise<TemplateSaveOutcome> {
  let id: string | null;
  try {
    id = await templateSnapshotId(deps.currentId, deps.ensureSaved);
  } catch {
    // The save itself blew up: it wrote nothing and showed nothing, so this is
    // the one path that WOULD have been silent.
    return "failed";
  }
  if (!id) return "skipped";
  try {
    return (await deps.snapshot(id)) ? "saved" : "failed";
  } catch {
    return "failed";
  }
}
