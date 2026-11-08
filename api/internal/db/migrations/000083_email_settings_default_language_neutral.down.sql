-- Restore the column DEFAULTs 000021 declared, so a downgrade leaves the schema
-- shaped exactly as the pre-000083 code expects (a fresh row then holds the
-- Malay literal again, which is what that code was written against).
--
-- The DATA is deliberately NOT re-filled: 000083 blanked only rows that still
-- held the exact pre-language literal, and re-filling would overwrite the
-- deliberate "not customised" state of every workspace created or cleared since
-- the upgrade. An empty half is rendered correctly by the pre-000083 code too
-- (dbstore.ResolveFormEmailSettings fills an empty half from the catalogue), so
-- the downgrade is safe without touching a single row.
ALTER TABLE workspace_email_settings
    ALTER COLUMN receipt_subject SET DEFAULT 'Resit pembayaran — {{ref_code}}';

ALTER TABLE workspace_email_settings
    ALTER COLUMN receipt_body SET DEFAULT $legacy$Terima kasih atas pembayaran anda!

Resit pembayaran JomForm
Rujukan : {{ref_code}}
Jumlah  : RM {{amount}}
Item    : {{items}}
Status  : Pembayaran diterima

Simpan emel ini sebagai resit rasmi anda.
Borang dibeli: {{form_url}}$legacy$;
