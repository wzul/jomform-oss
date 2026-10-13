import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Workspaces — JomForm Docs",
  description:
    "What a workspace owns, its public address, how you create one, and what moving one to the Trash (and deleting it for good) requires.",
  alternates: { canonical: "https://jomform.com/docs/workspaces" },
};

// The full text behind the summaries on /docs (t_5425c958): "Workspaces
// (your account's tenant)", "Renaming a workspace" and "Deleting a workspace".
// The first two arrived on main (e45ede2 / #682) after the /docs restructure was
// written, so the index carried their whole 231- and 251-word bodies; the rebase
// cuts them to summaries and moves the walk-through here, like every other cut
// entry. "Renaming a workspace" arrives with PR 691 — its 259-word body was a
// paragraph, which the /docs entry-shape guard (docsIndex.test.ts) refuses, so
// the walk-through lives here and the index keeps the one-line summary.
//
// The anchor names are part of the contract: /docs links to them by `#id`, the
// Settings → Workspace danger zone's inline "?" points at
// `/docs#delete-workspace`, the identity card's "?" points at
// `/docs#rename-workspace`, and the first-run workspace ask points at
// `/docs#workspaces`. The index slugs stay on the index itself.
type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "workspaces",
    title: "What a workspace is",
    body: [
      "A workspace holds your forms, products, sales, CHIP credentials, domains and settings. Every workspace owns its own account, so data never crosses between them, and each has its own public host: your forms are served from {slug}.jomform.com (or a custom domain you add).",
      "If you have no workspace yet, the dashboard asks you to create one the first time you sign in — name it and pick a URL slug, and you land inside it able to create a form straight away.",
      // t_e3ebe979 (the rebase onto main): this paragraph is main's #743 text,
      // corrected by #750 (the account-slug host no longer serves). It used to
      // live in the /docs entry's body; the restructure cuts that entry to a
      // summary, so the walk-through belongs here or it is lost.
      "If you joined before workspaces existed and already had forms but no workspace, JomForm creates that workspace for you on your next sign-in, naming it and its URL after the first five characters of your email address, and moves your existing forms into it so they appear in your dashboard straight away — nothing is deleted. Your forms then serve from that workspace's own address ({workspace-slug}.jomform.com): a form has exactly one public host, so if you had shared a link on an older address you will want to send the new one.",
      "Create more later from the workspace switcher in the top bar (\"New workspace\"); the switcher also sets which workspace you land on after login. A new workspace starts empty: no CHIP credentials, no products, no sales.",
    ],
  },
  {
    id: "rename-workspace",
    title: "Renaming a workspace",
    body: [
      // PR 691: this is the walk-through the /docs entry used to carry in full.
      // The name and the address are two different fields with two different
      // authorities, and the split is the part a merchant gets wrong.
      "A workspace's name and its public address can both be changed after it is created, in Settings → Workspace, where the identity card is editable.",
      "The NAME is only what you and your team see in the dashboard, the switcher and the workspace list, so it is safe to fix a typo or follow a rebrand. Anyone with the manage or admin role on the workspace can change it. It can be up to 120 characters, counted in characters rather than bytes — so a name in Chinese, Tamil, Arabic or with accents is measured the same way the field measures it, and 120 characters is 120 whatever the script. The same limit applies whether you create a workspace or rename one.",
      "The ADDRESS is different: <slug>.jomform.com is your workspace's public host, the thing printed on a QR stand, pasted into an iframe embed, and handed to an affiliate as a ?aff= link. Only the OWNER can change it — an admin member manages a workspace but does not own its public identity. The address must be 3–40 characters of a-z, 0-9 and dashes, cannot end in \"jomform\" (that is the product's own domain), and must not be in use by another workspace — including a workspace that has renamed away from it, because those former addresses still answer for their own merchant. A reserved name is refused with the reason stated; see the reserved workspace names below.",
      "Changing the address is safe by design: the old address is kept and keeps working, redirecting permanently (HTTP 308) to the new one, so a QR code already stuck to a table or a link already in an email does not break and nobody has to reprint anything. Unlike a form-slug rename, a former workspace address is never handed to another workspace — it always moves to the workspace that used to own it.",
      "Changing the address never requires touching the name: a request that changes only the address leaves the name exactly as it was, and an over-long name can never block an address change.",
      "The rename goes through PATCH /v1/workspaces/{id} — fields are optional, and an omitted one keeps its current value — or the MCP tool update_workspace, which requires confirm=true for an address change because moving a tenant's public identity is not something an agent should do by inference. The name alone needs no confirmation.",
    ],
  },
  {
    id: "delete-workspace",
    title: "Deleting a workspace",
    body: [
      // PR 804 (the workspace trash): a delete is a MARK, not a destroy. The
      // hard delete moved into Purge, which is a second, separate step.
      "A workspace is moved to the Trash with a typed confirmation — you type the workspace's own address (slug) back. Nothing is destroyed by that move: the workspace's forms, products, sales, payment account and API keys all stay exactly as they were, and it simply disappears from your workspace list and from the switcher.",
      "Two rules decide whether it may go. Only the OWNER may move it: an admin member manages a workspace but does not own its identity, its sales history or its payment account, and leaves through Settings → Members instead.",
      "You must have at least one other workspace, because a user always needs one — the last workspace can never go to the Trash, and the button is disabled with the reason stated rather than failing after you press it. A workspace that still holds content moves just as freely; there is nothing to empty first.",
      "From Trash you can restore a workspace whole, and its original address comes back with it — unless another workspace has taken that address while it sat in the Trash, in which case the restore is refused and you are told which name to deal with rather than having your workspace silently renamed.",
      "Deleting for good is a separate, second step: it only works on a workspace already in the Trash, it asks you to type the slug again, it refuses a workspace that has sales (sales are never destroyed or silently detached), and it cannot be undone.",
      "One thing it does not take with it: the payment account your sign-up created, when that account is older than the workspace — which it is for the account your first workspace was created onto. That account is detached from the destroyed workspace instead of being deleted, so the account and its CHIP credentials survive and a workspace you create later can use it again. Everything the workspace itself held goes with it.",
      "Settings → Workspace shows the identity and the Danger zone; the permanent step is reached through the REST API or an MCP client acting for the owner, never by clicking a dashboard control. AI agents use delete_workspace with check_workspace_deletion as the read-only pre-flight, list_trashed_workspaces and restore_workspace for the bin, and purge_workspace for the permanent step — which the in-app assistant can never propose.",
    ],
  },
];

export default function WorkspacesDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">Workspaces</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        A workspace is the tenant everything you build lives in. This page covers what it owns, its
        public address, how you get one, and exactly what moving one to the Trash — or deleting it
        for good — requires.
      </p>

      {SECTIONS.map((s) => (
        <section key={s.id} id={s.id} className="mt-10 scroll-mt-24">
          <h2 className="text-lg font-semibold text-slate-900">{s.title}</h2>
          {s.body.map((p, i) => (
            <p key={i} className="mt-3 text-sm leading-6 text-slate-600">
              {p}
            </p>
          ))}
        </section>
      ))}

      <p className="mt-10 text-sm leading-relaxed text-slate-600">
        See the{" "}
        <a
          href="/docs/customize#workspace-address"
          className="text-[var(--jf-primary)] underline underline-offset-2"
        >
          reserved workspace names
        </a>{" "}
        for the addresses you cannot claim, and the{" "}
        <a href="/docs/sales" className="text-[var(--jf-primary)] underline underline-offset-2">
          sales guide
        </a>{" "}
        for the history a delete refuses to discard.
      </p>
    </div>
  );
}
