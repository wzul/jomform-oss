-- Issue #390: add an 'unlisted' form status alongside draft | private |
-- published. An unlisted form is live (served at its direct /{slug} URL,
-- submissions work) but excluded from the space storefront / published
-- listing. This is a status value only — no new column — so we just relax
-- the forms.status CHECK constraint to allow 'unlisted'.

ALTER TABLE forms DROP CONSTRAINT IF EXISTS forms_status_check;
ALTER TABLE forms ADD CONSTRAINT forms_status_check
    CHECK (status IN ('draft','private','published','unlisted'));
