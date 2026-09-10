import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Placeholders reference — JomForm Docs",
  description:
    "Every dynamic placeholder you can use in JomForm emails and notifications: {admin_email}, {{ref_code}}, {{amount}}, {{items}}, {{buyer_name}}, {{form_url}}, {{date}} and any {{field_id}}.",
  alternates: { canonical: "https://jomform.com/docs/placeholders" },
};

// Single source of truth for the placeholder reference. Kept in sync with
// api/internal/payments/receipt.go and api/internal/payments/notifications.go.
const RECIPIENT_PLACEHOLDERS: { token: string; meaning: string }[] = [
  {
    token: "{admin_email}",
    meaning:
      "The merchant's account email. Used as a notification recipient — the email is resolved at send time.",
  },
  {
    token: "{field_id}",
    meaning:
      "The submitted value of a form field, used as a notification recipient (e.g. a buyer's email field). Replace field_id with the actual block id of the field.",
  },
];

const BODY_PLACEHOLDERS: { token: string; meaning: string }[] = [
  {
    token: "{{ref_code}}",
    meaning: "The sale's reference code (e.g. the code shown to the buyer and used to look up the sale).",
  },
  {
    token: "{{amount}}",
    meaning: "The formatted sale amount (e.g. 123.45 in the space's currency).",
  },
  {
    token: "{{items}}",
    meaning: "A summary of the purchased items (product names and quantities).",
  },
  {
    token: "{{buyer_name}}",
    meaning: "The buyer's name as submitted on the form.",
  },
  {
    token: "{{form_url}}",
    meaning: "The public URL of the form that was purchased.",
  },
  {
    token: "{{date}}",
    meaning: "The date the email is sent.",
  },
  {
    token: "{{field_id}}",
    meaning:
      "Any submitted form field value. Replace field_id with the actual block id of the field (e.g. {{email}} for a field whose id is email).",
  },
];

export default function PlaceholdersPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">Placeholders reference</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        JomForm emails and notifications support dynamic placeholders. When an email is sent, each
        placeholder is replaced with the real value for that sale or submission. There are two
        kinds: <code className="rounded bg-slate-100 px-1">{"{...}"}</code> tokens used as{" "}
        <em>recipients</em>, and <code className="rounded bg-slate-100 px-1">{"{{...}}"}</code>{" "}
        tokens used inside the subject and body.
      </p>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Recipient placeholders</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Used in the <em>Send to</em> field of a notification rule. A recipient is either a literal
        email address, <code className="rounded bg-slate-100 px-1">{"{admin_email}"}</code>, or a
        submitted field value.
      </p>
      <div className="mt-4 overflow-hidden rounded-lg border border-slate-200">
        <table className="w-full text-left text-sm">
          <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500">
            <tr>
              <th className="px-4 py-2 font-medium">Placeholder</th>
              <th className="px-4 py-2 font-medium">Meaning</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {RECIPIENT_PLACEHOLDERS.map((p) => (
              <tr key={p.token}>
                <td className="whitespace-nowrap px-4 py-3 font-mono text-xs text-[var(--jf-primary)]">
                  {p.token}
                </td>
                <td className="px-4 py-3 leading-relaxed text-slate-600">{p.meaning}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Subject & body placeholders</h2>
      <p className="mt-2 text-sm leading-relaxed text-slate-600">
        Used in the subject and body of receipt emails and notification rules.
      </p>
      <div className="mt-4 overflow-hidden rounded-lg border border-slate-200">
        <table className="w-full text-left text-sm">
          <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500">
            <tr>
              <th className="px-4 py-2 font-medium">Placeholder</th>
              <th className="px-4 py-2 font-medium">Meaning</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {BODY_PLACEHOLDERS.map((p) => (
              <tr key={p.token}>
                <td className="whitespace-nowrap px-4 py-3 font-mono text-xs text-[var(--jf-primary)]">
                  {p.token}
                </td>
                <td className="px-4 py-3 leading-relaxed text-slate-600">{p.meaning}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <h2 className="mt-8 text-xl font-semibold text-slate-900">Example</h2>
      <pre className="mt-3 overflow-x-auto rounded-lg bg-slate-900 p-4 text-xs leading-relaxed text-slate-100">
{`Subject: Jualan baharu — {{ref_code}}
Body:
  Rujukan : {{ref_code}}
  Jumlah  : RM {{amount}}
  Item    : {{items}}
  Pembeli : {{buyer_name}}
  Borang  : {{form_url}}`}
      </pre>
    </div>
  );
}
