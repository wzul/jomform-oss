export const PRIVACY_POLICY = {
  lastUpdated: "2026-09-01",
  company: "JomForm",
  email: "contact@jomform.com",
};

// Standard privacy policy for JomForm (SaaS order-form builder, MY-based users).
// Merchant-facing; buyers of merchant forms share the same principles.
export const PRIVACY_SECTIONS: { title: string; body: string[] }[] = [
  {
    title: "1. Who we are",
    body: [
      "JomForm (\"we\", \"us\") operates the website jomform.com and related services.",
      "We are a Malaysia-based online platform that lets merchants build order forms, accept payments through CHIP, and manage sales.",
      "For any privacy questions, contact contact@jomform.com.",
    ],
  },
  {
    title: "2. Data we collect",
    body: [
      "Account data: your name (if provided), email address, hashed password, two-factor authentication status, and account settings.",
      "Payment setup data: payment gateway credentials (CHIP Brand ID and secret key) which are encrypted at rest and never displayed in full after saving.",
      "Form content: the forms, products, themes, and settings you create.",
      "Sales data: orders placed on your forms, including the buyer's name, email, phone number, items purchased, and amounts — collected on behalf of you as the merchant.",
      "Buyer payment data: payment card details are processed entirely by CHIP and its payment partners. We never see or store full card numbers.",
      "Technical data: IP address, browser type, and pages visited, used for security, rate limiting, and analytics.",
      "Tracking scripts: if you enable third-party analytics (e.g. Google Analytics, Facebook Pixel), those providers collect data from your public form visitors under their own privacy policies.",
    ],
  },
  {
    title: "3. How we use data",
    body: [
      "To provide the service: host your forms, process orders, send receipts, and maintain your dashboard.",
      "Security: prevent abuse, enforce rate limits, verify administrators and API clients.",
      "Communication: service announcements, security notices, and (if you opt in) product updates.",
      "Legal compliance: e-Invoicing record-keeping and other obligations under Malaysian law.",
      "We do not sell your data or your buyers' data.",
    ],
  },
  {
    title: "4. Third parties we rely on",
    body: [
      "CHIP — payment processing (card and online banking data).",
      "Cloudflare — CDN, DNS, and DDoS protection for jomform.com.",
      "Amazon Web Services (SES) — transactional email delivery such as receipts.",
      "Google Analytics / Facebook Pixel — only when you, the merchant, enable them on your forms.",
      "Hosting and infrastructure providers for our servers.",
      "Each third party processes data under its own privacy policy and our data-processing instructions.",
    ],
  },
  {
    title: "5. Cookies",
    body: [
      "Essential cookies keep you signed in and protect against cross-site request forgery.",
      "Analytics or advertising cookies are only set on public forms if the merchant enables third-party tracking scripts.",
      "We do not use advertising cookies on the jomform.com dashboard itself.",
    ],
  },
  {
    title: "6. Data retention",
    body: [
      "Account and sales data are kept while your account is active.",
      "You may delete individual forms, products, or sales records at any time from the dashboard.",
      "If you request account deletion, we remove your personal data within 30 days, except records we must keep for tax or legal reasons.",
    ],
  },
  {
    title: "7. Your rights (PDPA & GDPR)",
    body: [
      "Access: request a copy of the personal data we hold about you.",
      "Correction: fix inaccurate data at any time from your dashboard.",
      "Deletion: ask us to erase your account and personal data.",
      "Portability: export your sales data as CSV at any time.",
      "Withdraw consent: disable optional tracking or delete your account.",
      "Malaysian residents may exercise rights under the Personal Data Protection Act 2010; EU/UK users have equivalent GDPR rights.",
    ],
  },
  {
    title: "8. Security",
    body: [
      "Passwords are stored as salted hashes (never in plain text).",
      "Payment gateway secrets are encrypted with AES-256-GCM.",
      "All traffic is served over HTTPS.",
      "SSH access to our servers is key-only; password logins are disabled.",
      "No system is perfectly secure — we monitor continuously and will notify affected users of any breach as required by law.",
    ],
  },
  {
    title: "9. Children",
    body: [
      "JomForm is not directed at children under 13 (or under 18 without parental consent for paid plans).",
      "We do not knowingly collect data from children. Contact us if you believe a child has provided data.",
    ],
  },
  {
    title: "10. Changes to this policy",
    body: [
      "We will post any changes on this page with a new \"last updated\" date.",
      "Material changes will be announced by email to account holders.",
    ],
  },
  {
    title: "11. Contact",
    body: [
      "JomForm — contact@jomform.com",
      "Effective date: 1 September 2026",
    ],
  },
];