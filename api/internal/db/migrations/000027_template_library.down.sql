-- Issue #174: remove the seeded template library + system account.
-- (Down migration is best-effort; only removes rows this migration created.)

DELETE FROM forms WHERE is_template AND slug IN (
  'tpl-fnb','tpl-booking','tpl-tuition','tpl-event','tpl-donation',
  'tpl-ecommerce','tpl-contact-simple','tpl-contact-advanced',
  'tpl-newsletter','tpl-survey','tpl-quote','tpl-employment',
  'tpl-gift','tpl-contest'
);

DELETE FROM accounts WHERE slug = 'jomform-templates';

DELETE FROM users WHERE email = 'templates@system.jomform';
