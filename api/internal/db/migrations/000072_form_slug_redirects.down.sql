-- Rollback of the form-slug rename redirects.
--
-- Dropping the table removes every temporary 301 a rename left behind; the
-- forms keep the slugs they were renamed to, so only the old-URL fallback goes
-- away (the old URLs then 404, which is the pre-rename behaviour for any slug a
-- form no longer serves).
DROP TABLE IF EXISTS form_slug_redirects;
