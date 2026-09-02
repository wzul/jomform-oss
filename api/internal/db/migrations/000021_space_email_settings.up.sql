-- Issue #107: per-space email template + behaviour settings.
-- One row per space (mirrors space_settings). Defaults reproduce the current
-- hardcoded receipt exactly, so existing behaviour is unchanged until a
-- merchant customises. receipt_body uses {{placeholders}} rendered
-- server-side (Go text/template / html/template) — never client-side.
CREATE TABLE IF NOT EXISTS space_email_settings (
    space_id              UUID PRIMARY KEY REFERENCES spaces(id) ON DELETE CASCADE,
    receipt_subject       TEXT NOT NULL DEFAULT 'Resit pembayaran — {{ref_code}}',
    receipt_body          TEXT NOT NULL DEFAULT 'Terima kasih atas pembayaran anda!

Resit pembayaran JomForm
Rujukan : {{ref_code}}
Jumlah  : RM {{amount}}
Item    : {{items}}
Status  : Pembayaran diterima

Simpan emel ini sebagai resit rasmi anda.
Borang dibeli: {{form_url}}',
    receipt_enabled       BOOLEAN NOT NULL DEFAULT true,
    notify_merchant       BOOLEAN NOT NULL DEFAULT false,
    merchant_notify_email TEXT NOT NULL DEFAULT '',
    html                  BOOLEAN NOT NULL DEFAULT false,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
