-- Card t_281831eb: a new workspace must not be born holding one language's
-- translation of the receipt.
--
-- 000021 gave both columns the MALAY rendering of the receipt as their DEFAULT,
-- so EVERY workspace — English, Chinese, Tamil, Arabic — stored a Malay subject
-- and body at the moment its row was materialised. The dashboard renders the
-- stored value verbatim (Settings -> Emel), so an English merchant saw Malay in
-- a field he never filled in, and the value looked like his own content.
--
-- The stored value is the SYSTEM's text, not merchant content, so it has no
-- business being pinned to one language: an EMPTY value means "JomForm's own
-- receipt, in the language each form is set to", which is exactly what the
-- senders already resolve (dbstore.ResolveFormEmailSettings fills an empty half
-- from emailtext.Text(lang, Receipt, nil); a stored half that still equals a
-- built-in template is re-rendered in the buyer's language). '' is also what the
-- dashboard can honestly show as "not customised".
--
-- The table is named workspace_email_settings here, NOT the space_email_settings
-- 000021 created it as: 000045 renamed it (and space_id -> workspace_id), so a
-- migration written against the old name fails on a fresh database — the exact
-- class of bug TestMigrationsApplyFromZero exists to catch.
--
-- The backfill judges each half on its own and compares it byte for byte with
-- the literal 000021 wrote (emailtext.LegacyDefault(emailtext.Receipt)): a
-- subject and a body are two separate merchant decisions, so editing one must
-- not pin the other, and a half the merchant actually typed must never be
-- rewritten. The rows this blanks are ALREADY classified as system text by the
-- sender (emailtext.SubjectIsSystemDefault / BodyIsSystemDefault), so no email
-- that goes out today changes language because of this migration — only the
-- dashboard stops presenting the system's Malay text as merchant content.
ALTER TABLE workspace_email_settings ALTER COLUMN receipt_subject SET DEFAULT '';
ALTER TABLE workspace_email_settings ALTER COLUMN receipt_body SET DEFAULT '';

-- The subject: the exact pre-language literal.
UPDATE workspace_email_settings
   SET receipt_subject = ''
 WHERE receipt_subject = 'Resit pembayaran — {{ref_code}}';

-- The body: the exact pre-language literal (dollar-quoted so the newlines are
-- literal and no escaping can drift from emailtext.LegacyDefault).
UPDATE workspace_email_settings
   SET receipt_body = ''
 WHERE receipt_body = $legacy$Terima kasih atas pembayaran anda!

Resit pembayaran JomForm
Rujukan : {{ref_code}}
Jumlah  : RM {{amount}}
Item    : {{items}}
Status  : Pembayaran diterima

Simpan emel ini sebagai resit rasmi anda.
Borang dibeli: {{form_url}}$legacy$;
