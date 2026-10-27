// The destination-fact tables behind `docsIndex.test.ts`'s "every cut entry's facts
// are readable at the destination" test.
//
// WHY THIS LIVES IN A MODULE OF ITS OWN (card t_5cfeb788)
//
// The test used to hold these tables and read each destination's `.tsx` through a
// string heuristic (`stripUnrendered` / `readerVisible`). Those heuristics have hit
// their limit: they can drop a comment, but "is this source reachable" and "does
// this branch render" are not decidable by string matching. A mutation that deletes
// every paragraph from the JSX and keeps the text in module-scope `const`s nothing
// references was measured GREEN 269/269 with `next build` exit 0 — while the built
// page contained the phrases 0 times. A fact must only pass if the page RENDERS it.
//
// The reliable check is the built HTML, and the built HTML is produced by a command
// the unit suite cannot run inside (see web/scripts/docsFacts.mjs). So the reader
// moved there, and BOTH readers — the unit test and the build-time check — import
// the tables from this one module. That is deliberate: two copies of these tables
// would drift, and the version that drifted would be the one deciding a verdict.

// The per-entry facts.
//
// Every entry here is one whose text was CUT on /docs and moved into a guide, so the
// guide is now the only place a reader can be told these things. The strings are the
// facts a reader would have to be told, not the entry's exact wording — a fact that
// asserted the wording would fail on a copy-edit rather than on substance.
const DESTINATION_FACTS: Record<string, string[]> = {
  // Shipping by zone (the managed rate source) and the courier providers that
  // quote it live. The facts a reader has to be able to find on the guide: the
  // MCP tools that configure both halves, and the "never a silent zero" rule that
  // decides an unmatched address (and a failed courier quote).
  "Shipping by zone": [
    "get_shipping_rates",
    "set_shipping_rates",
    "get_shipping_couriers",
    "connect_shipping_courier",
    "disconnect_shipping_courier",
    "never silently zero",
  ],
  "Sales": [
    "reference code",
    "pending/confirmed/failed/refunded",
    "confirm bank-transfer sales",
    "export sales to csv",
    "reports the mode as",
  ],
  "One list for sales and responses": [
    "one combined record count",
    "record_type",
    "the status filter is a payment question",
    "list_sales",
  ],
  "Responses (response-only forms)": [
    "mode to response in the builder",
    "detail drawer",
    "list_form_submissions",
  ],
  "Form analytics (the per-form counter)": [
    "Paid+Confirmed",
    "never added together",
    "get_form_analytics",
  ],
  "Partial refunds": ["refunded_sen", "outstanding_sen", "partially refunded", "per-sale lock"],
  "Subscriptions": [
    "billing period",
    "number of charges",
    "schedule mode",
    "retries automatically",
    "never revokes the customer's saved card token",
  ],
  "Realtime notifications & the order screen beep": [
    "sound_enabled",
    "Live pill",
    "get_realtime_status",
    "buyers are never played a sound",
  ],
  "Order forms": ["drag-and-drop builder", "products", "custom domain"],
  "Variation products": ["its own name, price, stock and image", "list_product_variations"],
  "Product images on forms": ["image-top", "product_image_size"],
  // The entry QA falsified. Every fact below was absent from the tree this
  // guard was written against; the last three are the gallery-metadata half
  // that had been dropped instead of moved.
  //
  // t_6a23c70d: the gallery-metadata fact changed sides. The page told merchants
  // the description was "the value of its first Text block" — the pre-t_7fb80479
  // rule, when `text` still meant prose. It is the intro line now (a Section
  // block), and the API's publish gate was widened in the same change to accept
  // it, so the three facts below moved with the copy: the block the card reads,
  // the instruction to the merchant, and the legacy Text block that still counts.
  "Templates": [
    "config.category",
    "first section block",
    "put a short intro line in a section block",
    "a text block also counts",
    "refused with a clear message",
    "bare title",
    "bulk publish skips such a template",
    "order forms, booking, donation, contact, survey and more",
  ],
  "Publishing a form": [
    "Open form",
    "Copy link",
    "something to sell",
    "bulk_forms",
    "acknowledge",
  ],
  "Restaurant menu (QR ordering)": [
    "set_menu_page",
    "generate_qr",
    "table number",
    "order form",
  ],
  "Menu basket (multi-item ordering)": [
    "add-to-cart",
    "quantity stepper",
    "Clear basket",
    "reload",
  ],
  "Custom domains": ["custom.jomform.com", "auto-create the cname", "Let's Encrypt"],
  "Email & notifications": [
    "resolution order is form → workspace → built-in default",
    "sale.paid",
    "response.submitted",
    "delivered → opened → clicked",
  ],
  "Affiliate marketing": ["?aff=CODE", "create_affiliate", "get_affiliate_sales"],
  "Spam protection": ["un-flag", "30 days", "per workspace", "spam_judge_enabled"],
  "Trusted devices (2FA)": [
    "trusted device",
    "Revoke",
    "revoke all / log out everywhere",
  ],
  "MCP (AI agents)": ["Model Context Protocol", "OAuth 2.1", "mcp.jomform.com/mcp"],
  "Design → AI agent → MCP customization": ["Canva", "custom_css", "upload_media"],
  "Embed on your site": ["jomform:height", "640px", "postMessage"],
  // t_5425c958: the four entries that arrived on main after the restructure. Each
  // is a summary on /docs with a guide carrying the walk-through, so each gets the
  // same treatment as the entries above: the facts a reader would have to be told
  // are asserted at the destination, chosen from what the guide actually says.
  //
  // "Workspaces (your account's tenant)" and "Deleting a workspace" both point at
  // the guide this rebase added (/docs/workspaces), whose two sections carry the
  // 231- and 251-word bodies main had on the index. The delete facts are the two
  // remaining conditions and the two things a merchant would otherwise discover
  // by pressing the button: the typed slug and the refusal to destroy sales.
  // PR 804 (the workspace trash) re-cut this entry — the delete is a mark, not a
  // destroy — so three facts moved with the copy: the workspace no longer has to
  // be empty, the permanent step is now a second (purge) step, and the last
  // workspace "can never go to the Trash" rather than "never be deleted".
  "Workspaces (your account's tenant)": [
    "CHIP credentials",
    "{slug}.jomform.com",
    "asks you to create one",
    "New workspace",
    // t_e3ebe979: main's #743/#750 prose — the legacy-provisioning chain and the
    // one-public-host rule — lived in the /docs entry's own body. The restructure
    // cuts that entry to a summary, so the paragraph moved to the guide; these
    // two facts are what keeps it from being cut again.
    "first five characters of your email address",
    "a form has exactly one public host",
  ],
  "Deleting a workspace": [
    "typed confirmation",
    "leaves through Settings → Members instead",
    "the last workspace can never go to the trash",
    "refuses a workspace that has sales",
    "deleting for good is a separate, second step",
    "check_workspace_deletion",
    // t_9fe4c36a: the purge's account rule. Deleting for good no longer destroys
    // an account that existed BEFORE the workspace (the signup account a
    // first-workspace create adopts) — it detaches it. The claim is here so the
    // sentence in the guide cannot be cut back to silence about it.
    "detached from the destroyed workspace instead of being deleted",
  ],
  // PR 691: the entry whose 259-word body the /docs restructure refuses. It is a
  // summary pointing at /docs/workspaces#rename-workspace, so its facts are the
  // things a merchant has to be told before changing an address they already
  // published: the two different authorities, what a reserved label does, and
  // that the old address keeps working (a permanent redirect, and — unlike a
  // form-slug rename — one that no other workspace can ever claim).
  "Renaming a workspace": [
    "counted in characters rather than bytes",
    "Only the OWNER can change it",
    "redirecting permanently (HTTP 308) to the new one",
    "never handed to another workspace",
    "requires confirm=true for an address change",
  ],
  "Two-factor authentication (2FA)": [
    "6-digit",
    "magic link",
    "Disabling 2FA also clears every trusted device",
    "single-use",
  ],
  "REST API": ["OpenAPI 3.1", "Settings → API keys", "readonly"],
  // Arrived on main (#679) after the restructure was written; the entry is a
  // summary with the walk-through on the customize guide, like every other cut
  // entry, so its facts are asserted at that anchor. They are the three things a
  // merchant has to be told before renaming a URL they already published: the
  // character rule, what the refusal is, and that the old URL keeps working
  // through a temporary redirect (never a permanent alias).
  "Form URLs and slugs (rename a form)": [
    "1–40 characters",
    "refused with an error",
    "temporary redirect (http 301)",
  ],
  // t_4c58a09e: this branch's own entry, at the count its facts list above
  // holds. The facts are the three things the Activity page's guide has to
  // carry for the entry to be a summary and not a deletion: that the trail is
  // per WORKSPACE (the whole point of the page), that the destructive actions
  // are what it exists for, and the agent-facing tool that reads the same table.
  "Activity log (audit trail)": [
    "The trail is per workspace, not per user",
    "the only place in the product where you can see afterwards that one happened",
    "list_events",
  ],
  // Issue #255's custom HTML form: the facts a merchant (or the agent acting for
  // them) can only learn here — the two argument names, that the body is
  // sandboxed, that validation is not enforced, and the narrow server-side check.
  //
  // t_f55ecdea: the last three pin the STORAGE contract, because the shipped page
  // stated it wrongly. It said "only registered ids are recorded … so an extra
  // input in the markup can never write into the response". The handler folds the
  // buyer identity into the same stored map AFTER customform.Collect
  // (httpapi/public.go, handleResponseSubmit) and the frame's bridge reads it from
  // a control the markup NAMES (name="full_name"/"email"/"phone", card t_754ed4b4)
  // or, when it names none, from the first input[type=text]/[email]/[tel] the
  // markup carries — so an unregistered text control's value is stored as
  // full_name, and a well-formed form stores full_name/email/phone keys that are
  // not registered ids. These facts are the three halves of the corrected
  // sentence; dropping any one of them lets the absolute come back.
  "Custom HTML forms": [
    "custom_html",
    "custom_fields",
    "sandboxed iframe",
    "validation is a hint",
    "required-only",
    "medan wajib tiada dalam HTML",
    "an unregistered name never becomes a field of its own",
    "full_name, email and phone",
    "its value is stored as full_name",
  ],
};

// The floor each entry's fact list must not fall below.
//
// A fact dropped from a list is a guard getting weaker in the same commit that made
// it weaker, so the count is pinned separately from the list: the list can be
// shortened to satisfy the assertion, and this is what notices.
const PER_ENTRY_FLOOR: Record<string, number> = {
  // Raised 3 → 6 in the shipping phase-2 commit: three courier tools joined the
  // guide and its fact list together (the floor exists to notice a list being
  // shortened to suit itself, so it moves WITH the list, in the same change).
  "Shipping by zone": 6,
  Sales: 5,
  "One list for sales and responses": 4,
  "Responses (response-only forms)": 3,
  "Form analytics (the per-form counter)": 3,
  "Partial refunds": 4,
  Subscriptions: 5,
  "Realtime notifications & the order screen beep": 4,
  "Order forms": 3,
  "Variation products": 2,
  "Product images on forms": 2,
  Templates: 8,
  "Publishing a form": 5,
  "Restaurant menu (QR ordering)": 4,
  "Menu basket (multi-item ordering)": 4,
  "Custom domains": 3,
  "Email & notifications": 4,
  "Affiliate marketing": 3,
  "Spam protection": 4,
  "Trusted devices (2FA)": 3,
  "MCP (AI agents)": 3,
  "Design → AI agent → MCP customization": 3,
  "Embed on your site": 3,
  // t_5425c958: the four entries this rebase added/kept, at the count each
  // DESTINATION_FACTS list above holds. The equality check below fails if a fact
  // is dropped without this floor moving with it.
  "Workspaces (your account's tenant)": 6,
  "Deleting a workspace": 7,
  // PR 691: the rename entry's summary, at the count its DESTINATION_FACTS list
  // above holds. The facts are the two authorities, the character-counted cap,
  // the permanent redirect and the no-claimant rule — the things the old
  // paragraph carried that a reader must still be able to find.
  "Renaming a workspace": 5,
  "Two-factor authentication (2FA)": 4,
  "REST API": 3,
  // t_e3ebe979: main's #679 entry, at the count its DESTINATION_FACTS list
  // above holds (the equality check below fails if a fact is dropped without
  // this floor moving with it).
  "Form URLs and slugs (rename a form)": 3,
  // t_4c58a09e: this branch's own entry, at the count its facts list above holds.
  "Activity log (audit trail)": 3,
  // Issue #255's guide, at the count its facts list above holds — including the
  // three storage facts t_f55ecdea added (the identity keys are written outside
  // the custom_fields filter, so "only registered ids are stored" was false).
  "Custom HTML forms": 9,
};

// parseFactTables pulls `title: "…"` and the following `href: "…"` out of a JSX
// source file, without needing the entry's `body`.
//
// It is a SEPARATE, SMALLER reader than docsIndex.test.ts's parseGroups on purpose.
// parseGroups must find every entry and asserts on each one's body; this only has to
// find the pairs, and a reader that demanded less would fail on entries the test
// legitimately handles. Both read the same text: the `/docs` index's GROUPS table.
//
// The string scanner is duplicated from the test file (it was a local function
// there) because the build-time reader cannot import from a `.test.ts` — node's test
// runner would then load the test file, and with it every assertion, from a build
// step. The duplication is of a 20-line scanner, not of the fact tables, which is
// where drift would actually cost a verdict.
export function jsString(s: string, i: number): [string, number] {
  // `i` is the index OF the opening quote.
  const q = s[i];
  let out = "";
  let j = i + 1;
  while (j < s.length) {
    const c = s[j];
    if (c === "\\") {
      const n = s[j + 1];
      if (n === "n") out += "\n";
      else if (n === "t") out += "\t";
      else out += n;
      j += 2;
      continue;
    }
    if (c === q) return [out, j + 1];
    out += c;
    j += 1;
  }
  throw new Error(`unterminated string literal starting at ${i}`);
}

export type FactTarget = { title: string; href?: string };

export function parseFactTargets(src: string): FactTarget[] {
  const start = src.indexOf("const GROUPS");
  if (start === -1) return [];
  const end = src.indexOf("\n];", start);
  const region = src.slice(start, end === -1 ? src.length : end);

  const titleAt: number[] = [];
  for (let pos = 0; ; ) {
    const t = region.indexOf('title: "', pos);
    if (t === -1) break;
    titleAt.push(t);
    pos = t + 1;
  }

  const out: FactTarget[] = [];
  for (let k = 0; k < titleAt.length; k++) {
    const from = titleAt[k];
    const to = k + 1 < titleAt.length ? titleAt[k + 1] : region.length;
    const block = region.slice(from, to);
    const [title] = jsString(region, from + 'title: "'.length - 1);
    const hrefAt = block.indexOf('href: "');
    const target: FactTarget = { title };
    if (hrefAt !== -1) {
      const [href] = jsString(region, from + hrefAt + 'href: "'.length - 1);
      target.href = href;
    }
    out.push(target);
  }
  return out;
}

// checkDestinationFacts compares the fact tables against READER-VISIBLE text.
//
// `readerTextOf` is passed in because the two callers get their text differently and
// that difference is the whole point of the card: the unit test passes the source
// page through `readerVisible` (which can only drop comments), while the build-time
// check passes the page's BUILT HTML, where unreferenced source simply is not
// present. The comparison itself is shared so a fact cannot pass one reader and fail
// the other for a reason other than the text they were handed.
//
// `hrefToKey` maps an href to the key the caller knows the text under; returning
// undefined means there is no text to check, which is reported rather than skipped.
export function checkDestinationFacts(opts: {
  targets: FactTarget[];
  hrefToKey: (href: string) => string | undefined;
  readerTextOf: (key: string) => string | undefined;
}): string[] {
  const missing: string[] = [];
  for (const t of opts.targets) {
    const facts = DESTINATION_FACTS[t.title];
    if (!t.href || !facts) continue;
    const key = opts.hrefToKey(t.href);
    if (key === undefined) {
      missing.push(`${t.title} → ${t.href} (no page)`);
      continue;
    }
    const text = opts.readerTextOf(key);
    if (text === undefined) {
      missing.push(`${t.title} → ${t.href} (no text to read)`);
      continue;
    }
    const lower = text.toLowerCase();
    for (const fact of facts) {
      if (!lower.includes(fact.toLowerCase())) missing.push(`${t.title} → ${t.href}: "${fact}"`);
    }
  }
  return missing;
}

export { DESTINATION_FACTS, PER_ENTRY_FLOOR };
