import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Email & notifications — JomForm Docs",
  description:
    "Receipt emails and per-form notification rules: how a form overrides the workspace, the default notification every new form starts with, dynamic placeholders, and delivery tracking.",
  path: "/docs/email",
});

// The full text behind the "Email & notifications" summary on /docs
// (t_d768342e): receipts, per-form notification rules, the default rule a new
// form starts with, and delivery tracking. /docs links to #notifications, and
// the builder's Form-mode "?" links there too, so that id stays put.
const SECTIONS: Section[] = [
  {
    id: "notifications",
    title: "Receipt emails and notification rules",
    icon: "✉️",
    body: [
      "Each workspace holds its own email settings, and a new workspace starts with subject and body empty. Any form can override them: subject, body, **receipt on/off** and merchant notify.",
      "Each field offers a **use workspace default** option, and the **resolution order is form → workspace → built-in default**.",
      {
        list: [
          "An empty subject or body is not a missing subject.",
          "It means JomForm's own receipt, in the language the buyer's form is set to: an **English form gets an English receipt**, a Malay form a Malay one.",
          "Your own words are sent exactly as written, in every language. **JomForm does not translate your text.**",
        ],
      },
      {
        list: [
          "Every new form starts with a **default notification** to you. Its subject and body start empty, like the receipt fields.",
          "A payment form notifies you on **sale.paid**.",
          "A response form notifies you on **response.submitted**, using the responder's name and email because a response has no sale.",
          "An untouched rule shows a **grey example**, not a wall of text you must delete.",
          "The half you leave alone follows the form's language.",
        ],
      },
      "Switching a form's mode re-points that default notification onto the event the new mode fires. A re-pointed default takes the **system's own label** for that event.",
      "Only the default rule moves, and only while its message is untouched. An edited subject, body or recipient keeps it as written. A form with deleted rules stays silent.",
      "**The label is a dashboard name, not a decision about the message**, so renaming the label alone does not opt the rule out.",
      "The [MCP tool reference](/docs/mcp) covers the email settings tools.",
    ],
  },
  {
    id: "placeholders",
    title: "Dynamic placeholders",
    icon: "🧩",
    body: [
      "The subject and body support **dynamic placeholders**: `{{ref_code}}`, `{{amount}}`, `{{items}}`, `{{buyer_name}}`, `{{form_url}}`, `{{date}}` and any `{{field_id}}`.",
      "Each one is replaced with the real value for that sale or submission when the email is sent.",
      "The [placeholders reference](/docs/placeholders) lists every one, including the `{admin_email}` recipient token.",
      "It also marks the placeholders that carry no value on a response form.",
    ],
  },
  {
    id: "delivery",
    title: "Delivery tracking",
    icon: "📬",
    body: [
      "Track every message: sent → **delivered → opened → clicked**. The status is per email, so a receipt that never arrived is not a message that was never sent.",
      "An email that could not be queued reads `enqueue_failed`. It never left the platform, and that is not a delivery state: no sent or delivered event will follow it.",
      "Filter the Email deliveries page by that status to find a buyer whose receipt was lost before it was ever attempted.",
      "Addresses that must never be mailed sit on the platform's suppression list. The [email suppression list page](/docs/ses-reputation) says what gets on it and how an address comes off.",
    ],
  },
];

export default function EmailDocsPage() {
  return (
    <DocsPage
      title="Email & notifications"
      lead="How a form overrides the workspace's email settings, what a new form starts with, the placeholders, and how delivery is tracked."
      sections={SECTIONS}
    />
  );
}
