import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Trusted devices (2FA) — JomForm Docs",
  description:
    "How a trusted device skips the 2FA code for 30 days, how to see every trusted device, and why revoking one is immediate.",
  path: "/docs/security",
});

// The full text behind the "Trusted devices (2FA)" summary on /docs
// (t_d768342e). /docs links to #trusted-devices, so that id stays put.
const SECTIONS: Section[] = [
  {
    id: "two-factor",
    title: "Two-factor authentication (2FA)",
    icon: "🔐",
    body: [
      "Add a second factor from **Profile → Security**. Scan the QR code into an authenticator app (Google Authenticator, 1Password, Authy…) and confirm the 6-digit code it shows.",
      "The secret is only activated once you confirm a code, so an **abandoned setup never locks you out**. From then on, each sign-in asks for the current 6-digit code.",
      {
        list: [
          "Tick **don't ask for a code on this device for 30 days** at login to trust that device.",
          "The password is still required every time.",
          "Profile → Security lists every trusted device, with a **Revoke** button per device and **revoke all / log out everywhere**.",
        ],
      },
      "To turn 2FA off, open Profile → Security and click **Disable 2FA**. You type the current 6-digit code from your authenticator app and nothing else.",
      "There is no separate password prompt, so it also works on a session you started from an emailed sign-in link.",
      "**Disabling 2FA also clears every trusted device**, because a device that skipped the removed factor must not keep that exemption.",
      "A session started from an emailed sign-in link (a **magic link**) is treated as unproven.",
      {
        list: [
          "With 2FA on, changing your password, disabling 2FA or adding a passkey asks for a fresh code.",
          "A stolen inbox cannot strip the second factor.",
          "Each code is **single-use**. If a prompt is submitted twice, use the next code your app shows.",
        ],
      },
    ],
  },
  {
    id: "trusted-devices",
    title: "Trusted devices",
    icon: "💻",
    body: [
      "When 2FA is on, tick **don't ask for a code on this device for 30 days** at login. That device is a **trusted device** and skips the 2FA code.",
      "The password is still required on every sign-in, trusted device or not.",
      "Profile → Security lists every trusted device with its label, IP, first trusted date and expiry, plus a **Revoke** button on each row.",
      "The same list carries **revoke all / log out everywhere**.",
      "Revocation is immediate: the next login on a revoked device asks for a 2FA code again.",
      "Changing your password or disabling 2FA also clears all trusted devices.",
      {
        note: "AI agents see and revoke the same list over MCP with `list_trusted_devices`, `revoke_trusted_device` and `revoke_all_trusted_devices`; the [MCP tool reference](/docs/mcp) lists the signatures.",
      },
    ],
  },
  {
    id: "login-sessions",
    title: "Sessions that need a fresh code",
    icon: "🕒",
    body: [
      "A session created through a magic link is marked as a **magic session**.",
      "With 2FA enabled, a sensitive action in that session asks for a fresh 2FA code:",
      { list: ["Changing your password.", "Disabling 2FA.", "Adding a passkey."] },
      "A normal password login is unaffected, and so is an account without 2FA.",
    ],
  },
  {
    id: "passkeys",
    title: "Passkeys (sign in without a password)",
    icon: "🔑",
    body: [
      "A **passkey** signs you in with your face, fingerprint or device PIN instead of a password. Register one from Profile → Security → Register passkey.",
      "Your browser asks your device to create it, and the passkey appears in the list with the date it was added.",
      "The login page offers a passkey button before you type anything, because a passkey sign-in starts from a list of your credentials rather than an email address.",
      "Press the button, approve the prompt, and you are in. **Nothing is typed.**",
      {
        list: [
          "Each passkey is bound to one device or password manager (iCloud Keychain, Google Password Manager, 1Password, a security key).",
          "Add one per device you sign in from.",
          "Remove one with the delete button on its row. **Deleting a passkey stops it working immediately**, and you can always sign in the ordinary way.",
        ],
      },
      {
        warn: "A passkey added before this flow was fixed may be **non-discoverable**, which the login page's button cannot use.",
      },
      "That row is what the login button refuses. Delete it and register a new passkey, which is made discoverable. **Re-registering is the only recovery.**",
      "Adding a passkey from a magic-link session asks for a fresh 2FA code, the same as changing your password or disabling 2FA.",
    ],
  },
];

export default function SecurityDocsPage() {
  return (
    <DocsPage
      title="Trusted devices & 2FA"
      lead="What a trusted device is, how long it lasts, how to see and revoke one, and which sessions still need a fresh code."
      sections={SECTIONS}
    />
  );
}
