-- Card t_0da81d94 — normalise the BM prose the old default confirmation wrote.
--
-- Every form's confirmation set is created with a default row, and until now that
-- row's message was the constant dbstore.DefaultConfirmationMessage, verbatim:
--
--     'Terima kasih! Maklum balas anda telah diterima.'
--
-- The submit path served that stored message to the buyer's page, so the
-- renderer's own localized fallback (Ls.ResponseReceived, one per locale) was
-- never reached: a workspace whose language is `en` thanked its buyers in Malay
-- under an English heading. Measured on prod while verifying t_41b9c427.
--
-- The built-in default is a DICTIONARY lookup, not prose the server owns, so the
-- constant is empty now and an empty message means "not merchant-authored —
-- resolve it in the page's language". This migration moves the existing rows to
-- that shape; without it the prose stays in the table and keeps being served.
--
-- Scope: is_default rows carrying the shipped sentence EXACTLY. A merchant-typed
-- message is left untouched (including a merchant who typed that same sentence
-- into a NAMED confirmation — those rows are not the built-in default). If a
-- merchant had typed it into the default row itself, the buyer gets the built-in
-- wording for the page's locale instead, which is the same message in the same
-- language for `ms`.
UPDATE form_confirmations
   SET message = ''
 WHERE is_default
   AND message = 'Terima kasih! Maklum balas anda telah diterima.';
