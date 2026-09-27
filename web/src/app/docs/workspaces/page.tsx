import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Workspaces — JomForm Docs",
  description:
    "What a workspace owns, its public address, how you create one, and the three conditions a permanent delete has to meet.",
  alternates: { canonical: "https://jomform.com/docs/workspaces" },
};

// The full text behind the two summaries on /docs (t_5425c958): "Workspaces
// (your account's tenant)" and "Deleting a workspace". Both arrived on main
// (e45ede2 / #682) after the /docs restructure was written, so the index carried
// their whole 231- and 251-word bodies; the rebase cuts them to summaries and
// moves the walk-through here, like every other cut entry.
//
// The anchor names are part of the contract: /docs links to them by `#id`, the
// Settings → Workspace danger zone's inline "?" points at
// `/docs#delete-workspace`, and the first-run workspace ask points at
// `/docs#workspaces`. Both index slugs stay on the index itself.
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
    id: "delete-workspace",
    title: "Deleting a workspace",
    body: [
      "A workspace is deleted with a typed confirmation — you type the workspace's own address (slug) back — because the delete is permanent and takes the workspace's payment account with it.",
      "Three rules decide whether it may go. Only the OWNER may delete it: an admin member manages a workspace but does not own its identity, its sales history or its payment account, and leaves through Settings → Members instead.",
      "You must have at least one other workspace, because a user always needs one — the last workspace can never be deleted, and the button is disabled with the reason stated rather than failing after you press it.",
      "And the workspace must be empty: forms (including anything still in the Trash), products, sales, custom domains, webhooks, live API keys and subscriptions each block it, and the refusal lists every blocking category with its count. Sales especially are never destroyed — a delete takes the account and its trading history with it, so sales block the delete instead of being silently discarded.",
      "To empty a workspace, open the Forms page, switch to Trash and delete a form for good; a form that has been sold refuses that permanent delete, because removing it would silently detach the sale.",
      "Settings → Workspace shows the identity and the Danger zone; AI agents use delete_workspace, with check_workspace_deletion as the read-only pre-flight that reports the same blockers.",
    ],
  },
];

export default function WorkspacesDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">Workspaces</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        A workspace is the tenant everything you build lives in. This page covers what it owns, its
        public address, how you get one, and exactly what deleting one requires.
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
