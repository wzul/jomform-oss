-- Issue #174: expand the form template library.
--
-- The template gallery (GET /v1/templates) lists published forms with
-- is_template=true. The existing 3 templates (tpl-fnb, tpl-booking,
-- tpl-tuition) were inserted manually and are not in the repo. This migration
-- seeds a full library covering common use cases (mirroring Gravity Forms'
-- list), plus a dedicated system account that owns them.
--
-- Templates are owned by a single "system" account so they never appear in a
-- merchant's own form list (ListByAccount is account-scoped) and so the
-- account_id NOT NULL constraint is satisfied. The account is created with a
-- random password hash (unusable) and a reserved slug; it is never exposed to
-- login or the dashboard.
--
-- Non-payment templates (contact, survey, newsletter, quote, employment,
-- gift, contest) are response-only (config.mode='response', issue #171) so
-- they collect answers without a payment step. Payment templates (fnb,
-- booking, tuition, event, donation, ecommerce) keep the default payment mode
-- with a product block the merchant fills in.

-- 1. System account that owns the template gallery.
INSERT INTO users (email, password_hash)
VALUES ('templates@system.jomform', '!system-template-account!')
ON CONFLICT (email) DO NOTHING;

INSERT INTO accounts (owner_user_id, name, slug)
SELECT u.id, 'JomForm Templates', 'jomform-templates'
FROM users u
WHERE u.email = 'templates@system.jomform'
ON CONFLICT (slug) DO NOTHING;

-- 2. Seed the template library (idempotent per slug).
INSERT INTO forms
  (account_id, slug, title, config, theme, status, is_template, allow_embed)
SELECT a.id, t.slug, t.title, t.config, t.theme, 'published', true, true
FROM accounts a
CROSS JOIN (VALUES
  -- ── Payment templates (default mode, product block) ─────────────────────
  ('tpl-fnb',
   'F&B Order + Payment',
   '{"mode":"payment","category":"Food & Beverage","blocks":[{"id":"b1","type":"text","value":"Order from our menu — pay online when you submit."},{"id":"b2","type":"product","value":"Menu item"},{"id":"b3","type":"name","value":"Full name","required":true},{"id":"b4","type":"email","value":"Email","required":true},{"id":"b5","type":"phone","value":"Phone number"},{"id":"b6","type":"textarea","value":"Special instructions"}]}'::jsonb,
   '{"primary":"#5e6ad2"}'::jsonb),
  ('tpl-booking',
   'Service Booking Deposit',
   '{"mode":"payment","category":"Booking","blocks":[{"id":"b1","type":"text","value":"Book your slot — a deposit secures your appointment."},{"id":"b2","type":"product","value":"Booking deposit"},{"id":"b3","type":"name","value":"Full name","required":true},{"id":"b4","type":"email","value":"Email","required":true},{"id":"b5","type":"phone","value":"Phone number"},{"id":"b6","type":"textarea","value":"Preferred date & time"}]}'::jsonb,
   '{"primary":"#0ea5e9"}'::jsonb),
  ('tpl-tuition',
   'Tuition Class Registration',
   '{"mode":"payment","category":"Education","blocks":[{"id":"b1","type":"text","value":"Register for tuition — pay the registration fee to confirm your seat."},{"id":"b2","type":"product","value":"Registration fee"},{"id":"b3","type":"name","value":"Student name","required":true},{"id":"b4","type":"email","value":"Parent email","required":true},{"id":"b5","type":"phone","value":"Parent phone"},{"id":"b6","type":"textarea","value":"Subject / level"}]}'::jsonb,
   '{"primary":"#8b5cf6"}'::jsonb),
  ('tpl-event',
   'Event Registration',
   '{"mode":"payment","category":"Events","blocks":[{"id":"b1","type":"text","value":"Register for our event — pay the ticket fee to confirm your place."},{"id":"b2","type":"product","value":"Event ticket"},{"id":"b3","type":"name","value":"Full name","required":true},{"id":"b4","type":"email","value":"Email","required":true},{"id":"b5","type":"phone","value":"Phone number"},{"id":"b6","type":"textarea","value":"Dietary / accessibility needs"}]}'::jsonb,
   '{"primary":"#f59e0b"}'::jsonb),
  ('tpl-donation',
   'Donation',
   '{"mode":"payment","category":"Non-profit","blocks":[{"id":"b1","type":"text","value":"Support our cause — every donation makes a difference."},{"id":"b2","type":"product","value":"Donation amount"},{"id":"b3","type":"name","value":"Full name","required":true},{"id":"b4","type":"email","value":"Email","required":true},{"id":"b5","type":"textarea","value":"Message (optional)"}]}'::jsonb,
   '{"primary":"#10b981"}'::jsonb),
  ('tpl-ecommerce',
   'eCommerce Checkout',
   '{"mode":"payment","category":"eCommerce","blocks":[{"id":"b1","type":"text","value":"Complete your order — we will ship after payment."},{"id":"b2","type":"product","value":"Product"},{"id":"b3","type":"name","value":"Full name","required":true},{"id":"b4","type":"email","value":"Email","required":true},{"id":"b5","type":"phone","value":"Phone number"},{"id":"b6","type":"textarea","value":"Shipping address"}]}'::jsonb,
   '{"primary":"#ef4444"}'::jsonb),
  -- ── Response-only templates (mode='response', no payment) ───────────────
  ('tpl-contact-simple',
   'Simple Contact',
   '{"mode":"response","category":"Contact","blocks":[{"id":"b1","type":"text","value":"Get in touch — we usually reply within a day."},{"id":"b2","type":"name","value":"Full name","required":true},{"id":"b3","type":"email","value":"Email","required":true},{"id":"b4","type":"phone","value":"Phone number"},{"id":"b5","type":"textarea","value":"Your message","required":true}]}'::jsonb,
   '{"primary":"#5e6ad2"}'::jsonb),
  ('tpl-contact-advanced',
   'Advanced Contact',
   '{"mode":"response","category":"Contact","blocks":[{"id":"b1","type":"text","value":"Tell us about your enquiry."},{"id":"b2","type":"name","value":"Full name","required":true},{"id":"b3","type":"email","value":"Email","required":true},{"id":"b4","type":"phone","value":"Phone number"},{"id":"b5","type":"textarea","value":"How can we help?","required":true},{"id":"b6","type":"checkbox","value":"I would like a call back"}]}'::jsonb,
   '{"primary":"#6366f1"}'::jsonb),
  ('tpl-newsletter',
   'Newsletter Signup',
   '{"mode":"response","category":"Marketing","blocks":[{"id":"b1","type":"text","value":"Join our newsletter for updates and offers."},{"id":"b2","type":"name","value":"Full name"},{"id":"b3","type":"email","value":"Email","required":true},{"id":"b4","type":"checkbox","value":"I agree to receive emails"}]}'::jsonb,
   '{"primary":"#06b6d4"}'::jsonb),
  ('tpl-survey',
   'Survey',
   '{"mode":"response","category":"Feedback","blocks":[{"id":"b1","type":"text","value":"We value your feedback — it takes 2 minutes."},{"id":"b2","type":"name","value":"Full name"},{"id":"b3","type":"email","value":"Email"},{"id":"b4","type":"textarea","value":"How satisfied are you?","required":true},{"id":"b5","type":"textarea","value":"What could we improve?"}]}'::jsonb,
   '{"primary":"#14b8a6"}'::jsonb),
  ('tpl-quote',
   'Request a Quote',
   '{"mode":"response","category":"Sales","blocks":[{"id":"b1","type":"text","value":"Tell us what you need and we will send a quote."},{"id":"b2","type":"name","value":"Full name","required":true},{"id":"b3","type":"email","value":"Email","required":true},{"id":"b4","type":"phone","value":"Phone number"},{"id":"b5","type":"textarea","value":"Describe your project","required":true}]}'::jsonb,
   '{"primary":"#f97316"}'::jsonb),
  ('tpl-employment',
   'Employment Application',
   '{"mode":"response","category":"HR","blocks":[{"id":"b1","type":"text","value":"Apply for a role with us."},{"id":"b2","type":"name","value":"Full name","required":true},{"id":"b3","type":"email","value":"Email","required":true},{"id":"b4","type":"phone","value":"Phone number"},{"id":"b5","type":"textarea","value":"Position applying for","required":true},{"id":"b6","type":"textarea","value":"Tell us about your experience"}]}'::jsonb,
   '{"primary":"#64748b"}'::jsonb),
  ('tpl-gift',
   'Gift Certificate',
   '{"mode":"response","category":"Gifts","blocks":[{"id":"b1","type":"text","value":"Request a gift certificate."},{"id":"b2","type":"name","value":"Recipient name","required":true},{"id":"b3","type":"email","value":"Recipient email","required":true},{"id":"b4","type":"textarea","value":"Message to include"},{"id":"b5","type":"checkbox","value":"Send me a copy"}]}'::jsonb,
   '{"primary":"#ec4899"}'::jsonb),
  ('tpl-contest',
   'Contest Entry',
   '{"mode":"response","category":"Contests","blocks":[{"id":"b1","type":"text","value":"Enter our contest — good luck!"},{"id":"b2","type":"name","value":"Full name","required":true},{"id":"b3","type":"email","value":"Email","required":true},{"id":"b4","type":"phone","value":"Phone number"},{"id":"b5","type":"textarea","value":"Your entry","required":true}]}'::jsonb,
   '{"primary":"#a855f7"}'::jsonb)
) AS t(slug, title, config, theme)
WHERE a.slug = 'jomform-templates'
  AND NOT EXISTS (
    SELECT 1 FROM forms f
    WHERE f.slug = t.slug AND f.is_template
  );
