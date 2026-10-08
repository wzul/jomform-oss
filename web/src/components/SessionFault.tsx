"use client";

// The dashboard's SESSION-BOOTSTRAP FAULT surface (card t_d1c9a43a).
//
// A `dashboard-bundle` read that produced NO ANSWER — a 5xx, a dropped
// connection, a 200 that is not JSON, or a read that never answered and was
// bounded (session.tsx's BUNDLE_READ_TIMEOUT_MS) — is a FAULT, never a
// sign-out: `web/src/components/session.tsx` reports it as `sessionError` with
// `bundle === undefined`, deliberately NOT as `anon`. That is the whole point of
// #1041 (card t_48319be9): `anon` is reached ONLY from an authoritative
// refusal (a 401, or a 403 with no tenancy code), because folding a fault into
// it bounced every page to /landing and read as a logout the server never asked
// for.
//
// THE GAP THIS CLOSES. #1041 put the fault + retry surface on ONE route: `/`,
// whose page is `dashboard-client.tsx`. Every other dashboard route read `anon`
// from the session context and nothing else, so a settled fault left
// `bundle === undefined`, the route stayed on its own loading branch, and the
// visitor got a sentence-less "…" with no way to retry. Measured by QA on the
// released build (v1.20.75, tree 3308cc72): a settled 5xx on /chat, /forms,
// /settings, /products and /sales read faultSentence=0 retryBtn=0, and the same
// shape holds for a HUNG read past the 20s bound — while the identical probe on
// `/` read faultSentence=1 retryBtn=1.
//
// SO THE SURFACE IS ONE COMPONENT, RENDERED FROM ONE PLACE every dashboard route
// already passes through: the app chrome (`Shell`), which returns this INSTEAD
// of the page's content when the bootstrap faulted. A route cannot forget it,
// because it does not have to remember it — a new page is covered the moment it
// renders inside the shell. `/` renders this same component from its own branch
// (a fault on `/` never reaches Shell).
//
// IT IS KEYED ON `sessionError` AND MUST NEVER BE KEYED ON `anon`: rendering
// this for an authoritative 401 would replace the wanted /landing bounce with a
// dead page offering a retry that can never succeed. `src/lib/sessionFault.test.ts`
// pins that both ways, and the CONTROL arm of
// `web/e2e/session-bootstrap-anon.spec.ts` measures the 401 behaviour.

import { useI18n } from "./i18n";
import { useSession } from "./session";

/**
 * The fault + retry surface, as a full-page state.
 *
 * It re-reads `sessionError` from the context rather than trusting its caller,
 * so it can never paint a "Retry" over a healthy session: a caller that gets the
 * condition wrong renders NOTHING here rather than lying about the session.
 * `retry()` re-attempts the bootstrap read (session.tsx) — it is the one action
 * that can recover the state, and it is offered wherever the visitor landed.
 */
export function SessionFault() {
  const { t } = useI18n();
  const { sessionError, retry } = useSession();
  // Not a fault — no surface. Every caller may render this unconditionally.
  if (!sessionError) return null;
  return (
    <main className="flex min-h-screen items-center justify-center px-6 text-center">
      <div>
        <p className="text-sm text-[var(--jf-muted)]">{t("common.sessionUnavailable")}</p>
        <button type="button" className="jf-btn-primary mt-4" onClick={retry}>
          {t("common.retry")}
        </button>
      </div>
    </main>
  );
}
