-- Issue #597: repair the default notification on RESPONSE-mode forms.
--
-- Background. EnsureDefault pre-creates a default rule for every new form, and
-- migration 000032 back-filled one for every existing form. Both wrote a rule
-- hardcoded to `sale.paid`. That is correct for a payment form, which records a
-- sale. A RESPONSE-mode form (an RSVP, an event registration, a contact form)
-- records a `submissions` row and never records a sale, so `sale.paid` on it can
-- never fire: the rule existed, was enabled, and silently did nothing — the
-- merchant was never told anyone had responded.
--
-- This migration moves those orphaned default rules onto the event that
-- actually fires for a response form, `response.submitted`, and replaces the
-- sale-flavoured message with one whose placeholders carry a real value for a
-- submission. A response has no reference code, amount or items.
--
-- SCOPE — deliberately narrow, so no merchant-authored rule can be seized:
--   - only the form's own DEFAULT rule (`is_default` is set only by
--     EnsureDefault and by the 000032 back-fill; the notification editor never
--     sets it, and an edited rule keeps the flag),
--   - only while it still carries the exact template the system wrote
--     (`event`, `subject`, `body` AND `to_email` all unchanged) — a merchant who
--     changed any of those made a decision, and this migration leaves it alone,
--   - only on a non-deleted RESPONSE-mode form. Payment forms keep `sale.paid`
--     untouched: 31 such rules are live in production and are correct.
--
-- A merchant who deleted their default rule keeps zero rules and stays silent:
-- the opt-out contract of EnsureDefault is not reversed here (nothing is
-- INSERTed, only an existing dead rule is repaired).
--
-- The rule this migration implements is the same one EnsureDefault now applies
-- at form creation, so a form created after this release and a form repaired by
-- it end up identical.
UPDATE form_notifications n
   SET event      = 'response.submitted',
       name       = 'Admin Notification',
       subject    = 'Pendaftaran baharu diterima',
       body       = E'Pendaftaran baharu diterima!\n\nNama   : {{full_name}}\nEmel   : {{email}}\nBorang : {{form_url}}\nTarikh : {{date}}',
       updated_at = now()
  FROM forms f
 WHERE f.id = n.form_id
   AND f.config->>'mode' = 'response'
   AND f.deleted_at IS NULL
   AND n.is_default = true
   AND n.event = 'sale.paid'
   AND n.to_email = '{admin_email}'
   AND n.subject = 'Jualan baharu — {{ref_code}}'
   AND n.body = E'Jualan baharu diterima!\n\nRujukan : {{ref_code}}\nJumlah  : RM {{amount}}\nItem    : {{items}}\nPembeli : {{buyer_name}}\nBorang  : {{form_url}}';
