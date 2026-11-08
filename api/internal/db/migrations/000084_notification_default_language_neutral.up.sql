-- Card t_1768cb11: a default notification rule must not be born holding one
-- language's translation of the message.
--
-- 000032 gave every form a default notification rule whose subject and body were
-- the MALAY rendering of the message, and 000062 rewrote that rule for
-- response-mode forms in Malay too. The dashboard renders the stored value
-- verbatim (form settings -> Notifications -> a rule -> Edit, bound at
-- web/src/app/form-settings/page.tsx), so an English merchant opening a rule he
-- never wrote saw Malay in Subject and Body, and it read as his own content.
--
-- The stored value is the SYSTEM's text, not merchant content, so it has no
-- business being pinned to one language. An EMPTY value means "JomForm's own
-- message, in the form's language": payments.sendNotification already re-renders
-- a stored half that equals a built-in template in the buyer-facing language, and
-- it now treats an empty half the same way. Blanking the row is therefore a
-- display fix with no change to any delivered email.
--
-- Each half is judged on its own and compared byte for byte with the literal the
-- pre-language release wrote (emailtext.LegacyDefault for NotificationSale and
-- NotificationResponse): a subject and a body are two separate merchant
-- decisions, so editing one must not pin the other, and a half the merchant
-- actually typed is never rewritten. The rows this blanks are ALREADY classified
-- as system text by the sender (emailtext.SubjectIsSystemDefault /
-- BodyIsSystemDefault), so no email that goes out today changes language because
-- of this migration — only the dashboard stops presenting the system's Malay text
-- as merchant content.
--
-- is_default is deliberately NOT part of the predicate, matching the sender: the
-- classification is by BYTES, not by how the row was created. A rule the merchant
-- added himself whose text is exactly the system's own is the system's text
-- however it got there, and it is re-rendered at send time exactly the same way.

-- The sale notification: 000032's default, still on every payment form.
UPDATE form_notifications
   SET subject = ''
 WHERE subject = 'Jualan baharu — {{ref_code}}';

UPDATE form_notifications
   SET body = ''
 WHERE body = $legacy$Jualan baharu diterima!

Rujukan : {{ref_code}}
Jumlah  : RM {{amount}}
Item    : {{items}}
Pembeli : {{buyer_name}}
Borang  : {{form_url}}$legacy$;

-- The response notification: 000062's repair, on every response-mode form.
UPDATE form_notifications
   SET subject = ''
 WHERE subject = 'Pendaftaran baharu diterima';

UPDATE form_notifications
   SET body = ''
 WHERE body = $legacy$Pendaftaran baharu diterima!

Nama   : {{full_name}}
Emel   : {{email}}
Borang : {{form_url}}
Tarikh : {{date}}$legacy$;
