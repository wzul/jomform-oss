-- Rollback for 000062_response_form_default_notification.
--
-- Reverses exactly what the up migration changed, under the same guards: the
-- form is a non-deleted response-mode form and the rule is its default one now
-- carrying the response template this migration wrote. A rule that was already
-- a merchant customisation was never touched, and is never touched here either.
--
-- The effect of the rollback is the pre-#597 behaviour on those rows: the
-- default rule on a response form goes back to `sale.paid`, which cannot fire
-- for a form that records submissions rather than sales.
UPDATE form_notifications n
   SET event      = 'sale.paid',
       name       = 'Admin Notification',
       subject    = 'Jualan baharu — {{ref_code}}',
       body       = E'Jualan baharu diterima!\n\nRujukan : {{ref_code}}\nJumlah  : RM {{amount}}\nItem    : {{items}}\nPembeli : {{buyer_name}}\nBorang  : {{form_url}}',
       updated_at = now()
  FROM forms f
 WHERE f.id = n.form_id
   AND f.config->>'mode' = 'response'
   AND f.deleted_at IS NULL
   AND n.is_default = true
   AND n.event = 'response.submitted'
   AND n.to_email = '{admin_email}'
   AND n.subject = 'Pendaftaran baharu diterima'
   AND n.body = E'Pendaftaran baharu diterima!\n\nNama   : {{full_name}}\nEmel   : {{email}}\nBorang : {{form_url}}\nTarikh : {{date}}';
