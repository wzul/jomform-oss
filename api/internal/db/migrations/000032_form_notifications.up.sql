-- Issue #193: per-form email notifications with conditional logic + default
-- (Gravity-Forms-style), replacing the flat per-space email settings for form
-- events. One row per notification rule. A default notification (notify the
-- merchant on sale.paid) is pre-created for every form; the merchant can edit
-- it or delete it per form (a form with zero notifications sends no email).
--
-- `to_email` is the recipient: a literal address, `{admin_email}` (the
-- merchant's account email, resolved at send time), or `{field_id}` (the
-- submitted value of a form field, e.g. a buyer email field).
-- `condition` is JSONB: {field_id, op, value} | null. Ops: equals |
-- not_equals | contains | gt | lt | not_empty.
CREATE TABLE IF NOT EXISTS form_notifications (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    form_id    UUID NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT '',
    event      TEXT NOT NULL DEFAULT 'sale.paid'
               CHECK (event IN ('sale.paid','sale.failed','response.submitted','subscription.charged')),
    to_email   TEXT NOT NULL DEFAULT '',
    subject    TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    condition  JSONB,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_form_notifications_form ON form_notifications(form_id);

-- Backfill: pre-create the default notification (notify merchant on sale.paid)
-- for every existing form, so existing behaviour (merchant gets a new-sale
-- email) is preserved until a merchant customises or deletes it per form.
INSERT INTO form_notifications (form_id, name, event, to_email, subject, body, is_default)
SELECT id,
       'Admin Notification',
       'sale.paid',
       '{admin_email}',
       'Jualan baharu — {{ref_code}}',
       'Jualan baharu diterima!

Rujukan : {{ref_code}}
Jumlah  : RM {{amount}}
Item    : {{items}}
Pembeli : {{buyer_name}}
Borang  : {{form_url}}',
       true
FROM forms
WHERE deleted_at IS NULL;
