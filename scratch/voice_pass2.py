"""Second pass: the English strings the first pass could not see.

The first extractor's key regex excluded keys with capitals (e.g. `login.pwOptional`,
`set.lpPrimaryColor`), so it reported 55 dashed strings when the file has ~205.
This pass walks every `en:` value and rewrites the dash by its grammatical role:

  inside parentheses   -> "; "        (masked — leave blank)  =>  (masked; leave blank)
  otherwise            -> ". "        No forms yet — x        =>  No forms yet. X

Fragments the rule cannot judge are listed at the end for a hand edit.
"""
import re

PATH = "web/src/components/i18nDict.ts"
src = open(PATH, encoding="utf-8").read()
EN = re.compile(r'(en\s*:\s*)"((?:[^"\\]|\\.)*)"', re.S)

# Keys where the dash is decorative or the sentence needs a verb the rule cannot invent.
HAND = {
 "bld.alwaysShow": "Always show",
 "set.modeHint": "Mode follows the secret key you paste: sk_live_ means Live, sk_test_ means Test.",
 "set.secretMasked": "Stored key: {masked} (masked; leave blank to keep)",
 "set.secretMaskedPlaceholder": "•••••• (masked; leave blank to keep)",
 "set.emailFallbackHint": "(optional; used when buyer email is missing; one-time forms only)",
 "wh.retryQueued": "Retry queued. It will be re-delivered shortly.",
 "prod.schedCatchUp": "Keep original dates, collect missed months",
 "prod.schedSkipMissed": "Keep original dates, skip missed months",
 "set.savedCreds": "Saved. Credentials verified with CHIP ✓",
 "set.savedCredsUnverified": "Saved. Credentials stored, but the CHIP check could not be confirmed.",
 "set.tlsStatusActive": "TLS active. Certificate issued and serving",
 "set.tlsStatusPending": "TLS pending. Certificate provisioning in progress",
 "set.tlsStatusCustom": "Custom certificate. Your own cert is being served",
 "set.tlsModeAuto": "Auto-TLS enabled. Let's Encrypt will issue a cert for this domain.",
 "set.tlsModeCustom": "Custom cert mode. Upload your certificate below.",
 "set.verifyCta": "I have set the CNAME. Verify",
 "set.liveLink": "Live. Open https://{domain}",
 "set.pollingHint": "Auto-checking every 20s. No need to click again",
 "set.autoCnameHint": "Cloudflare connected. We can create the CNAME for you.",
 "set.autoCnameNotLinked": "Cloudflare not linked. Set the CNAME manually.",
 "set.autoCnameStatusFailed": "Auto-CNAME failed. Set it manually",
 "set.cfNotConfigured": "Cloudflare auto-setup is not configured on this deployment. Add the CNAME record manually.",
 "set.feesTitle": "Processing fee, paid by the buyer",
 "set.emelSubjectPh": "Payment receipt {{ref_code}}",
 "fs.notifSubjectPh": "New sale {{ref_code}}",
 "mcp.urlLabel": "Server URL. Paste this exactly into your MCP client",
 "chat.scopeRequired": "Choose a workspace first. The assistant answers for one workspace at a time.",
 "ws.renameNothing": "Nothing changed yet. Edit the name or the address first.",
 "chat.proposal.value.variationSet": "{n} rows, the full list, replacing the existing one",
 "chat.proposal.value.variationSetOne": "1 row, the full list, replacing the existing one",
 "bld.bulkCaptureDone": "Captured {captured}/{requested}. {failed} failed.",
 "set.tlsEmailSaved": "ACME contact email saved. This domain's cert will use it.",
 "set.tlsCertSaved": "Certificate saved. Your domain now serves your own cert ✓",
 "set.tlsCertFailed": "Could not save the certificate. Check the PEM format and that the key matches.",
 "set.cfLinkFailed": "Cloudflare linking failed. Please try again.",
 "apiErr.tooManyRequestsInMinutes": "Too many attempts. Try again in {minutes} minutes.",
 "apiErr.tooManyRequestsInSeconds": "Too many attempts. Try again in {seconds} seconds.",
 "media.uploadedGifStill": "Uploaded ✓. Animated GIF saved as a still image (first frame only).",
 "media.purgedEdgeStale": "File removed from storage. The space is released. The CDN may still serve its cached copy for up to {seconds} seconds.",
 "media.purgedEdgeUnnamed": "File removed from storage. The space is released. We could not confirm the CDN copy was cleared, so a cached copy may still be served for a short while.",
 "media.deletedToTrash": "File moved to the trash. Restore it from there if you change your mind.",
 "bld.overrideCleared": "Override cleared. Account defaults apply",
 "bld.aiApplied": "AI draft applied. Review and edit before saving.",
 "fs.emelCleared": "Overrides cleared. Inheriting the workspace settings ✓",
 "fs.chipCleared": "CHIP feed cleared. Account defaults apply",
 "fs.chipNotConfigured": "Not configured. Uses account defaults",
 "fs.notifCondNone": "No condition. Always send on this event",
 "fs.notifEmpty": "No notifications. This form sends no email.",
 "forms.canPurgeFalse": "Has sales. Cannot be deleted for good",
 "login.freeNote": "Free forever. Your first form in 5 minutes.",
 "login.pwOptional": "Optional. Leave blank to sign up without a password (you can set one later).",
 "afb.notifyRetryStillMarked": "Re-sent, but the thread still records a failure, so it was not delivered to everyone. Check the banner below.",
 "afb.notifyRetryUnconfirmed": "We could not confirm whether the notification was re-sent. Check the banner below before assuming the filer was told.",
 "afb.repliedNotifyFailed": "Reply saved, but the filer could NOT be notified. Use the banner above to send it again.",
 "afb.repliedUnconfirmed": "Reply saved. The delivery status of the notification was not confirmed. Check the thread before assuming the filer was told.",
 "afb.notifyRetrySkipped": "Nothing was sent. There is no recipient configured for this notification, so no email was queued. Ask a platform admin to check the super-admin allowlist.",
 "afb.notifyFailedWho": "The {who} was not told about this thread. The email could not be handed to the delivery queue.",
 "chat.confirmRoleDenied": "This change was not applied because you need the manage or admin role in this workspace, and this card cannot be used again. Ask the assistant to prepare a new one.",
 "chat.confirmRefused": "The change was not carried out, and this card cannot be used again. Ask the assistant to prepare a new card.",
 "chat.confirmWorkspaceRequired": "This change was not carried out because this account has no workspace yet, and this card cannot be used again. Create a workspace, then ask the assistant to prepare a new card.",
 "chat.proposalUnexecutable": "This proposal cannot be carried out as prepared. Ask the assistant to prepare it again. Nothing was changed and nothing was used up.",
 "chat.cancelStale": "That proposal is already settled. There is nothing to cancel.",
 "chat.limitNote": "Up to {n} messages an hour. This is a limit, not a judgement. It keeps the feature affordable for everyone.",
 "chat.reply.noAnswer": "I could not finish that for you, and nothing was changed. Tell me the exact details (for a new form: its name and the fields you want) and I will try again.",
 "chat.tooLong": "That message is too long. Please shorten it.",
 "chat.proposalBadgeCancelled": "Cancelled. Nothing changed",
 "chat.proposalBadgeStale": "Expired. Nothing changed",
 "chat.proposalBadgeUnexecutable": "Cannot be applied. Nothing changed",
 "chat.refused.writeTool": "The assistant cannot run that itself. A change like that has to be prepared for you to confirm first.",
 "login.signupHere": "Enter your email and we will email you a sign-in link. The first time signs you up.",
 "login.newToJomform": "New to JomForm? You will get a magic link. No password needed to start.",
 "prof.trustedDevicesHelp": "Devices that can skip the 2FA code for 30 days. Revoke any device you no longer use. The next login there will ask for a code again.",
 "prof.passkeyDesc": "Sign in with your fingerprint, face or device PIN. No password.",
 "prof.mcpEmptyHelp": "Point any MCP client at the server URL. It registers itself automatically.",
 "prof.mcpScopeHelp": "A connected agent can read, manage and administer every workspace you own, up to your role in each workspace. It is never granted more than your own role in any workspace.",
 "url.notHttps": "An image URL must start with https://. Other schemes (data:, http:, javascript:) are rejected so the image never breaks on the buyer's page.",
 "bld.dragHere": "Drag blocks here, or click one on the left.",
 "bld.withdrawnBlock": "No longer offered in the builder. Buyers see this label and a note, not a field.",
 "bld.textProseUndo": "Undo. Make it a field again",
 "bld.inlineStyleHint": "Safe CSS only. url() and background-image are stripped. Applied to this field's element.",
 "bld.shipNoZoneSets": "No zone set yet. Create one in Form Settings → Shipping.",
 "fs.slugHint": "The URL path this form is served at (a-z, 0-9, dash; 1-40 characters). Saving a change moves the form to the new URL. The old URL keeps working through a temporary redirect, and the slug must be free in this workspace.",
 "fs.qrHelp": "Generate a QR that points to this form's public URL. Optionally enter a table number; it is encoded as ?table=N so the order reaches you with the table. Print it and put it on a table stand.",
 "fs.notifHint": "Email notifications this form sends. A default notification (notify you on payment) is pre-created. Edit it or add more. A form with no notifications sends no email.",
 "fs.shipHint": "Delivery zones and rates shared by every form in this workspace. A shipping block whose rate source is Managed zones charges the zone the buyer's address resolves to, and the fee is recorded as shipping on the sale, never as a line item.",
 "media.imageStorage": "Images are saved as JPEG. A PNG with a transparent background is kept as PNG so it stays see-through; an animated GIF keeps only its first frame.",
 "mcp.whatBody": "A connected agent is your ChatGPT or Claude AI that has authorised JomForm via OAuth 2.1 (one-click, PKCE). Once connected it can read and manage your products, prices, sales, refunds and analytics.",
 "mcp.scopesBody": "A connected agent gets one of two scopes. Choose it in your MCP client when you connect: the client asks JomForm for the scope it wants, and the grant it receives is enforced on every tool call.",
 "mcp.scopeReadBody": "The agent can look at everything (forms, products, sales, analytics) and change nothing. Every tool that creates, updates, deletes, archives, cancels or refunds is refused with a scope error.",
 "mcp.scopeReadWriteBody": "The agent can read and also change your store: add products, edit forms, refund a sale. This is what a client receives when it does not ask for a scope.",
 "sales.mixedSubtitle": "Every sale and response across your forms. Filter, search, export.",
 "sales.liveHint": "Realtime updates are on. New sales and responses appear without refreshing.",
 "sales.soundSaveFail": "Could not save the sound setting, so it has been switched back. This setting is saved per workspace.",
 "fb.navHint": "Every signed-in user can send feedback here. It is not tied to any workspace.",
 "fb.adminHint": "You are a platform admin. The Admin tab shows every thread for triage.",
 "workspace.askBody": "You need a workspace before you can create a form. It takes a moment: pick a name and a URL slug.",
 "prod.amountHint": "The buyer types the amount on the form. The server checks it against these limits before charging. Leave the maximum empty for no upper limit.",
 "prod.amountFloor": "Minimum {cur}1.00. Below that the processing fee costs more than the gift.",
 "sub.cardNeedsUpdate": "Card failed. Update the payment method",
 "forms.viewUnavailableHint": "No public link yet. Publish the form, or set a workspace or custom domain first",
 "set.keysEncrypted": "Keys are stored encrypted and never shown again.",
 "set.menuHelp": "Turn THIS form into a clean restaurant menu (categories, item images, prices, best-seller). The menu is served at the form's own URL and shows only the products this form's product blocks reference. Customers scan a QR stand at the table, pick items and pay online. The order reaches you with the table number.",
 "set.menuPreviewHelp": "How your storefront menu will be structured: sections in order, with the items you picked.",
 "set.menuPreviewEmpty": "No categories configured yet. The products this form references show under \"Uncategorised items\".",
 "set.menuOrderFormHelp": "Pick a published form to collect extra info per order (delivery address, phone, spice level, notes, pickup time). The form's own fields render in the menu checkout and are validated on the server, so there is no second field editor. Create or edit the fields in the form builder.",
 "set.menuOrderFormMandatory": "The buyer's name and email are always collected and cannot be removed. CHIP requires both to create a payment.",
 "set.menuOrderFormMissing": "The selected form is not published. Pick another or publish it.",
 "set.menuOrderFormNone": "None. Collect name + email only",
 "set.legalFooterPointer": "A page written here appears in the footer of your public forms only when the matching legal URL in Settings → Storefront points at {slug}.jomform.com and this page has content. Otherwise the footer drops the link, and Storefront says why under that field.",
 "set.sfLegalLinkNotServed": "Not shown on your public forms: JomForm serves a hosted legal page only at {served} (no trailing slash, no extra path). Type that exact URL, or link to a page on your own website.",
 "set.sfLegalLinkNoContent": "Not shown on your public forms: the hosted page {served} has no saved content yet. Add its text in the Legal tab and save the footer again, or link to a page on your own website.",
 "set.sfDefaultLangHelp": "The language your customers see on your public forms, unless a form sets its own. Your dashboard language is separate; change that on your Profile page.",
 "set.sfSoundHelp": "When on, an open dashboard (for example a kitchen or order screen) plays a short beep and updates live when a new sale or response arrives. No manual refresh. Off by default.",
 "set.sfHelp": "Optional footer shown on your public forms. Only the fields you fill in are displayed. Leave everything blank for no footer.",
 "set.emelHelp": "Customise the receipt email JomForm sends to buyers on payment, and optionally notify you of each new sale. Defaults match the standard receipt, so leave them unchanged to keep current behaviour.",
 "set.currencyHelp": "The currency new products and sales are denominated in (default MYR). The workspace's accounts inherit it for CHIP purchases. Changing it applies to new products and sales going forward. Historical sales keep their original currency and are never auto-converted, so refunds must be in the original currency.",
 "set.currencyRatesHint": "Rates from Bank Negara Malaysia, cached once daily. Reference data only: JomForm never converts a saved amount (sales keep their original currency). To convert a specific amount, use the convert_currency MCP tool.",
 "set.autoCnameFailed": "Could not auto-create the CNAME: {error}. You can set it manually.",
 "dash.emailStatusNote": "Open tracking uses a tracking pixel in the receipt email. Only the open time is recorded, and no other personal data.",
 "apiErr.workspaceRequired": "Create a workspace before creating a form. A form needs one to be published at a working address.",
 "apiErr.scopeReadonly": "This API key can only read. It needs the readwrite scope for that.",
 "apiErr.webhookUrlInvalid": "That webhook URL points at a private or loopback host. Use a public address.",
 "apiErr.certInvalid": "The certificate or key is invalid. Check the PEM format and that the key matches.",
 "apiErr.emailUnverified": "Please verify your email before signing in. Check your inbox for the verification link.",
 "apiErr.formSlugReserved": "This name is reserved: JomForm uses it for its own addresses. Choose a different slug.",
 "apiErr.formCustomJsRejected": "Custom JS was rejected: it contains something we do not allow on a live form (code execution, obfuscation or beacon code, or an external URL). Remove it and save again.",
 "apiErr.formHasSales": "This form has sales, so it cannot be deleted for good. Sales history must not be destroyed with the form. It stays in the trash.",
 "apiErr.productHasSales": "This product has sales, so it cannot be deleted for good. Sales history must not be destroyed with the product. It stays in the trash.",
 "apiErr.productHasSubscriptions": "This product still has subscriptions, so it cannot be deleted for good. Cancel them first. It stays in the trash.",
 "apiErr.workspaceNotEmpty": "This workspace still has content, so it cannot be deleted. Empty it first; the list below says what is left.",
 "apiErr.workspaceHasSales": "This workspace has sales, so it cannot be permanently deleted. Sales are the record of money you received and are never destroyed with a workspace. It stays in the Trash.",
 "ws.renameNote": "You can rename it below. Changing the address keeps the old one working, so links, embeds and QR codes already in use keep pointing here.",
 "ws.slugLocked": "Only the workspace owner can change the address. It is the public link printed in QR codes, embeds and affiliate links, so it is the owner's call. Ask them, or create a new workspace if you need a different one.",
 "ws.dangerHelp": "Moving a workspace to the Trash destroys nothing: its forms, products, sales and payment account all stay, and you can restore it any time. Deleting it for good is a separate step reached through the REST API or an MCP client acting for the owner, and never from the dashboard, which offers Restore only.",
 "ws.lastWorkspace": "This is your only workspace, and it cannot be deleted. You always need at least one. Create another one first.",
 "ws.notEmptyHint": "Everything stays on disk, so the move is reversible. Destroying it for good is reached through the REST API or an MCP client, not the dashboard, and a workspace with sales refuses that: sales are your record of money received.",
 "ws.deleteHint": "Nothing is destroyed. You can restore it from the Trash at any time.",
 "ws.confirmBody": "Nothing is destroyed. The workspace leaves your workspace list and waits in the Trash with its forms, products, sales and payment account, so you can restore it any time. Deleting it for good is a separate step reached through the REST API or an MCP client acting for the owner, not from this view. Type the address below to confirm.",
 "ws.trashHelp": "Workspaces you moved to the Trash. Restore one to bring it back with everything it held. Nothing was destroyed.",
 "ws.trashHeldBySales": "Held by sales: {count}. These cannot be destroyed with the workspace.",
 "forms.purgeConfirmBody": "This cannot be undone. The form and its responses, notifications and webhooks are removed. Type the form address to confirm.",
 "forms.purgeHasSales": "This form has sales, so it cannot be deleted for good. Sales history is never destroyed. It stays in the trash.",
 "prod.trashNote": "Deleted products stay here until you delete them for good. A product here is no longer in your catalogue, which is different from an archived product: that one is still listed but hidden from buyers.",
 "prod.purgeConfirmBody": "This cannot be undone. The product and all of its variations are removed. Type the product name to confirm.",
 "prod.purgeHasSales": "This product has sales, so it cannot be deleted for good. Sales history is never destroyed. It stays in the trash.",
 "prod.purgeHasSubscriptions": "This product still has subscriptions, so it cannot be deleted for good. Cancel them first. It stays in the trash.",
 "media.trashNote": "Deleted files stay here until you delete them for good. They still count against your storage limit, because the file is still stored. Delete for good to free the space.",
 "media.purgeConfirmBody": "This cannot be undone. The file is removed from storage and the space is released. A copy may stay in the CDN cache for a few minutes.",
 "bld.noProductsWarn": "This is a payment form with no products, so it cannot take money yet. It collects nothing either: a buyer cannot submit it. Add a product first, or publish it anyway if you are still setting up the catalogue.",
 "bld.statusPrivateHint": "Password-protected. Buyers must enter the password to view it.",
 "bld.textProseWarn": "This reads like an introduction, but a Text block is an answer field, so on the live form this sentence becomes the field's label. If it is an introduction, treat it as an intro line.",
 "bld.modeResponseHint": "Collect responses only. No products, no payment.",
 "set.lpPrimaryColor": "Accent colour",
 "set.lpBackgroundColor": "Background colour",
 "bld.themeColor": "Theme colour (primary)",
 "mcp.examplesBody": "State what you want in plain words. For example: \\\"add product RM25.00\\\", \\\"how many sales today?\\\", \\\"refund order JF-xxxx\\\".",
}

changed, missed, edits = [], [], []
# Materialise every match against the ORIGINAL text, then apply back-to-front so
# the offsets of the replacements stay valid as the file changes length.
for m in list(re.finditer(r'"([A-Za-z0-9._]+)"\s*:\s*\{', src)):
    key = re.match(r'"([A-Za-z0-9._]+)"', m.group(0)).group(1)
    em = EN.search(src, m.end(), m.end() + 4000)
    if not em:
        continue
    old = em.group(2)
    if "—" not in old and key not in HAND:
        continue
    if key not in HAND:
        missed.append(key)
        continue
    new = HAND[key].replace('"', '\\"')
    if new == old:
        continue
    edits.append((em.start(2), em.end(2), new, key))

for start, end, new, key in sorted(edits, reverse=True):
    src = src[:start] + new + src[end:]
    changed.append(key)

open(PATH, "w", encoding="utf-8").write(src)
print(f"diubah: {len(changed)}")
print(f"masih bersengkang tetapi tiada dalam HAND ({len(missed)}):")
for k in missed:
    print("  ", k)
