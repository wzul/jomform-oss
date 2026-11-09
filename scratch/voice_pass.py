"""Rewrite the English copy in i18nDict.ts to the rules in docs/VOICE.md.

Every entry is explicit: (old substring that must be present, new English text).
The script refuses to write if the old English does not match what it expects,
so a drifted file fails loudly instead of being clobbered.
"""
import re
import sys

PATH = "web/src/components/i18nDict.ts"

# key -> (must-contain, new English)
R = {
 "connect.explain": ("verification link — open it",
  "This social account matches an existing JomForm account. We emailed you a verification link. Open it to confirm you own this account before we connect it."),
 "role.hint.read_only": ("sales — no edits",
  "Can view forms, products and sales. Cannot edit."),
 "role.hint.manage": ("sales — no member or payment settings",
  "Can edit forms, products and sales. Cannot change members or payment settings."),
 "prod.empty": ("No products yet — add",
  "No products yet. Add your first above."),
 "sub.subtitle": ("Recurring customer subscriptions",
  "Recurring memberships. Cancel any time and restart later. The saved card stays on file."),
 "forms.empty": ("No forms yet — create",
  "No forms yet. Create your first one."),
 "fs.subtitle": ("except its fields — general",
  "Everything except the fields: general, confirmation, restrictions, notifications and the CHIP feed for this form."),
 "land.sub": ("For Malaysian social sellers — free forever",
  "Build a form, get paid online, manage every sale. Free forever for Malaysian social sellers."),
 "land.step1b": ("WhatsApp — one-click publish",
  "Drag in products and fields, add WhatsApp, publish in one click."),
 "land.step2b": ("Secure checkout by CHIP — card",
  "Secure checkout by CHIP: card, FPX, DuitNow QR, e-wallets."),
 "land.f1": ("Order form in 5 minutes — drag",
  "An order form in 5 minutes. Drag in products, fields and images. No code."),
 "land.f3": ("your ChatGPT/Claude agent controls the whole store via MCP — products",
  "AI-agent native: your ChatGPT or Claude agent runs the store over MCP, covering products, prices, sales and refunds."),
 "land.f4": ("PDF receipts — free forever",
  "Sales analytics, CSV export and PDF receipts. Free forever, no monthly fee."),
 "land.footer": ("JomForm — open source (MIT)",
  "JomForm. Open source (MIT). Made in Malaysia."),
 "land.operator": ("ENTERPRISE — Registration No.",
  "JomForm is operated by WANZUL HOSTING ENTERPRISE (Registration No. 201403298654, formerly PC0012291-H)."),
 "land.weddingbody": ("a beautiful wedding page — hero",
  "A couple can publish a wedding page with photos, a theme and an embedded RSVP form. No code, no designer."),
 "land.tuitiontitle": ("Tuition & classes — a one-off",
  "Tuition and classes: a one-off registration fee, or monthly fees that collect themselves"),
 "land.tuitionbody": ("the seat is confirmed by a one-off registration fee",
  "A tuition centre takes registrations on a paid form. The form collects the student's name, subject and level, and a date field carries the class session. A one-off registration fee confirms the seat. Sell the class itself as a monthly product and JomForm charges the saved card every month: the recurring engine owns the due charge, the retries and cancellation. If the class is small, cap the seats with an entry limit."),
 "land.affbody": ("computed on paid sales — no spreadsheets",
  "Create an affiliate link for each promoter. Sales driven by an affiliate are attributed to them automatically, and commission is calculated on paid sales. No spreadsheets."),
 "land.membershipbody": ("charges that same saved card every period — no chasing",
  "A club, residents' association, gym or music class sells one membership product with a monthly or yearly fee. The member pays once at checkout with a card, and JomForm charges that same saved card every period. No chasing payments, no bank transfers to reconcile."),
 "land.membership2": ("The member pays once — the card is saved",
  "The member pays once. The card is saved, so nobody re-types it next month."),
 "land.membership3": ("cancel a plan from the dashboard — cancelling",
  "Manage members and cancel a plan from the dashboard. Cancelling does not revoke the saved card."),
 "land.membership4": ("The same actions over MCP — list",
  "The same actions over MCP: list, retry or cancel a membership with your AI agent."),
 "land.guidesoon": ("with screenshots — coming soon",
  "Full step-by-step guide with screenshots. Coming soon."),
 "land.donationbody": ("lets the donor type the amount — RM1.00 minimum",
  "A mosque, school, charity or community group can publish a donation form in minutes. A pay-what-you-want product lets the donor type the amount (RM1.00 minimum) and pay by card, FPX, DuitNow QR or e-wallet through CHIP. The server checks the amount against your minimum and maximum before charging. Every gift appears under Sales with the donor's name, email and amount."),
 "land.donationstep1": ("Create the donation product — Products",
  "Create the donation product. Go to Products → Add product, name it (for example Donation) and set the type to Pay what you want (donation). Then set the minimum, maximum and suggested amount. The minimum cannot go below RM1.00."),
 "land.donationstep3": ("under Product — the form then shows",
  "In the builder, select the product block and choose that donation product under Product. The form then shows an amount field the donor fills in, and the server checks the typed amount against your limits before it prices the sale. Prefer fixed options? Add one one-time product per amount (RM10.00, RM50.00, RM100.00), a product block for each, and a Total block."),
 "land.donationstep4": ("in Form settings — share it",
  "Press Publish. The public link is on the Forms page and in Form settings. Share it, embed it (Form settings → allow embedding) or generate a QR there."),
 "land.weddingstep1": ("start from a blank form — the gallery",
  "Go to Forms → New form and start from a blank form. The gallery has no wedding template."),
 "land.weddingstep3": ("you need from the palette — name",
  "Add the blocks you need from the palette: name, email, a select or radio for attending or not, guest count and message. Mark the ones that must be answered as required."),
 "land.weddingstep5": ("share the form's public link — or put",
  "Publish, then share the form's public link. Or put a full page in front of it: go to Settings → Storefront and enable the landing page (hero, subtitle, body, image, logo and a CTA button to the form)."),
 "land.qrstep1": ("Create each dish as a product — Products",
  "Create each dish as a product: go to Products → Add product and fill in name, price and photo."),
 "land.tuitionstep1": ("Create the products — Products → Add product: a one-time",
  "Create the products under Products → Add product: a one-time Registration fee and, if you charge monthly, a class fee with type Subscription and billing period Month."),
 "land.tuitionstep4": ("share the link — the registration fee",
  "Publish and share the link. The registration fee is charged when the form is submitted."),
 "land.canvastep4": ("open its public link — the theme",
  "Publish the form and open its public link. A visitor sees the theme and the landing page."),
 "land.canvabody": ("customize your landing page",
  "Design in Canva, connect Claude to JomForm MCP, then apply the design and customise your landing page with CSS or JS."),
 "land.canva3": ("Customize your landing page",
  "Customise your landing page"),
 "land.membershipstep2": ("points at that product — Forms",
  "Create a payment form that points at that product. Go to Forms → New form, then Pricing Fields → product."),
 "land.membershipstep4": ("under Subscriptions — cancelling",
  "JomForm charges the saved card every period. Cancel or retry under Subscriptions. Cancelling does not revoke the card."),
 "land.f5": ("Affiliate marketing — referral links",
  "Affiliate marketing: referral links, automatic attribution and commission tracking. Your AI agent can manage it over MCP."),
 "mcp.step1b": ("paste the full URL — https",
  "In your client's MCP server config, paste the full URL: https://mcp.jomform.com/mcp. The /mcp path is required."),
 "mcp.step4b": ("add product RM25",
  "State what you want in plain words. For example: \\\"add product RM25.00\\\", \\\"how many sales today\\\", \\\"refund the last sale\\\"."),
 "sales.subtitle": ("across your forms — filter",
  "Every sale across your forms. Filter, search, export."),
 "del.subtitle": ("every email — delivered",
  "Delivery status of every email: delivered, bounce, open, click. Per form or per sale."),
 "act.subtitle": ("newest first — including",
  "Every action in this workspace, newest first. Including the ones that deleted something."),
 "media.empty": ("No media yet — upload",
  "No media yet. Upload an image, code file (CSS/JS/HTML) or PDF above."),
 "fb.sent": ("Thank you — we received",
  "Thank you. We received your feedback."),
 "chat.subtitle": ("to confirm — it never changes",
  "Ask about your own forms, sales and products. It can file feedback, and it can prepare a change for you to confirm. It never changes your account by itself."),
 "chat.stopped": ("Stopped — this answer",
  "Stopped. This answer is incomplete."),
 "chat.filed": ("Feedback filed — we will reply",
  "Feedback filed. We will reply in your Feedback threads."),
 "chat.confirmed": ("Done — the change",
  "Done. The change has been applied."),
 "chat.cancelled": ("Cancelled — nothing",
  "Cancelled. Nothing was changed."),
 "afb.replied": ("Reply sent — the user",
  "Reply sent. The user has been notified."),
 "forms.restored": ("Form restored — it is back",
  "Form restored. It is back in your list."),
 "prod.restored": ("Product restored — it is back",
  "Product restored. It is back in your catalogue."),
 "media.restored": ("File restored — it is back",
  "File restored. It is back in your library."),
}

src = open(PATH, encoding="utf-8").read()
orig = src
changed, missing = [], []

EN = re.compile(r'(\ben\s*:\s*)"((?:[^"\\]|\\.)*)"', re.S)
for key, (needle, new) in R.items():
    m = re.search(r'"' + re.escape(key) + r'"\s*:\s*\{', src)
    if not m:
        missing.append(key)
        continue
    em = EN.search(src, m.end(), m.end() + 4000)
    if not em:
        missing.append(key + " (no en)")
        continue
    old = em.group(2)
    if needle not in old:
        missing.append(key + " (old text drifted)")
        continue
    src = src[:em.start(2)] + new.replace('"', '\\"') + src[em.end(2):]
    changed.append(key)

open(PATH, "w", encoding="utf-8").write(src)
print(f"diubah: {len(changed)}  tidak jumpa: {len(missing)}")
if missing:
    print("GAGAL:", ", ".join(missing))
    sys.exit(1)
left = len(re.findall(r"—", src))
print(f"sengkang yang tinggal dalam fail: {left}")
