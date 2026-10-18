-- Reverse of 000076_form_confirmation_default_language.
--
-- Restores the pre-fix shape: a default confirmation row whose message is the
-- server's BM sentence. Only rows the up-migration emptied are touched, so a
-- default a merchant deliberately cleared stays cleared — nothing else can
-- produce an empty `is_default` message except this card's own change, whose
-- built-in default is the dictionary lookup the old code did not have.
UPDATE form_confirmations
   SET message = 'Terima kasih! Maklum balas anda telah diterima.'
 WHERE is_default
   AND message = '';
