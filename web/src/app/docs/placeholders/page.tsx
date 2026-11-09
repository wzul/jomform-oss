import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Placeholders reference — JomForm Docs",
  description:
    "Every dynamic placeholder you can use in JomForm emails and notifications: {admin_email}, {{ref_code}}, {{amount}}, {{items}}, {{buyer_name}}, {{form_url}}, {{date}}, {{table_number}} and any {{field_id}}.",
  path: "/docs/placeholders",
});

// Single source of truth for the placeholder reference. Kept in sync with
// api/internal/payments/receipt.go and api/internal/payments/notifications.go.
const SECTIONS: Section[] = [
  {
    id: "recipient-placeholders",
    title: "Recipient placeholders",
    icon: "📧",
    body: [
      "Used in the **Send to** field of a notification rule. A recipient is a literal email address, `{admin_email}`, or a submitted field value.",
      {
        table: {
          label: "Recipient placeholders",
          head: ["Placeholder", "Meaning"],
          rows: [
            [
              "`{admin_email}`",
              "The merchant's account email, used as a notification recipient. The address is resolved at send time.",
            ],
            [
              "`{field_id}`",
              "The submitted value of a form field, used as a recipient (a buyer's email field, for example). Replace field_id with the field's block id.",
            ],
          ],
        },
      },
      {
        note: "A recipient can also be a literal address: the Send to field takes one exactly as you type it.",
      },
    ],
  },
  {
    id: "body-placeholders",
    title: "Subject and body placeholders",
    icon: "📨",
    body: [
      "Used in the **subject and body** of receipt emails and notification rules. Each token is replaced with the real value for that sale or submission.",
      {
        table: {
          label: "Subject and body placeholders",
          head: ["Placeholder", "Meaning"],
          rows: [
            [
              "`{{ref_code}}`",
              "The sale's **reference code**, shown to the buyer and used to look up the sale. Empty on a response form: a response has no sale.",
            ],
            [
              "`{{amount}}`",
              "The formatted sale amount (123.45, in the workspace's currency). Empty on a response form.",
            ],
            [
              "`{{items}}`",
              "A summary of the purchased items (product names and quantities). Empty on a response form.",
            ],
            [
              "`{{buyer_name}}`",
              "The buyer's name as submitted on the form. On a response form this is the responder's name.",
            ],
            [
              "`{{full_name}}`",
              "The name of the person who submitted, buyer or responder. Always defined: empty when the submission carried no name.",
            ],
            [
              "`{{email}}`",
              "The email of the person who submitted. Always defined: empty when the submission carried no email.",
            ],
            ["`{{form_url}}`", "The public URL of the form that was purchased."],
            ["`{{date}}`", "The date the email is sent."],
            [
              "`{{table_number}}`",
              "The table number a QR-ordering customer scanned (the ?table=N on the order form URL). Empty for a regular online order.",
            ],
            [
              "`{{field_id}}`",
              "Any submitted form field value. Replace field_id with the field's block id: a field with id email is written `{{email}}`.",
            ],
          ],
        },
      },
      { heading: "Example" },
      {
        code: "Subject: New sale {{ref_code}}\nBody:\n  New sale received!\n\n  Reference : {{ref_code}}\n  Amount    : RM {{amount}}\n  Item      : {{items}}\n  Buyer     : {{buyer_name}}\n  Form      : {{form_url}}",
      },
      {
        note: "A **response form** gets a different default notification, because a response has no sale: its rule fires on `response.submitted` and uses only placeholders with a value.",
      },
      {
        note: "A placeholder with no value is sent as an empty string, so check the table above before using a sale token on a response form.",
      },
    ],
  },
];

export default function PlaceholdersPage() {
  return (
    <DocsPage
      title="Placeholders reference"
      lead="Every dynamic placeholder in JomForm emails and notifications, and the value each one carries for a sale or a submission."
      sections={SECTIONS}
    />
  );
}
