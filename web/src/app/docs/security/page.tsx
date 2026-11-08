import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";

export const metadata: Metadata = docsMetadata({
  title: "Trusted devices (2FA) — JomForm Docs",
  description:
    "How a trusted device skips the 2FA code for 30 days, how to see every trusted device, and why revoking one is immediate.",
  path: "/docs/security",
});

// The full text behind the "Trusted devices (2FA)" summary on /docs
// (t_d768342e). /docs links to #trusted-devices.
type Section = { id: string; title: string; body: string[] };

const SECTIONS: Section[] = [
  {
    id: "two-factor",
    title: "Two-factor authentication (2FA)",
    body: [
      "Add a second factor to your account from Profile → Security. Set up 2FA by scanning the QR code into an authenticator app (Google Authenticator, 1Password, Authy…) and confirming the 6-digit code it shows — the secret is only activated once you confirm a code, so an abandoned setup never locks you out. From then on, each sign-in asks for the current 6-digit code.",
      "You can tick “don't ask for a code on this device for 30 days” at login to trust that device (the password is still required every time); Profile → Security lists every trusted device with a Revoke button per device and “revoke all / log out everywhere”.",
      "To turn 2FA off, open Profile → Security and click Disable 2FA: you are asked for the current 6-digit code from your authenticator app, and that is the only thing you need to type — there is no separate password prompt, so it also works on a session you started from an emailed sign-in link. Disabling 2FA also clears every trusted device, because a device that skipped the removed factor must not keep that exemption.",
      "A session started from an emailed sign-in link (a magic link) is treated as unproven: when 2FA is on, changing your password, disabling 2FA or adding a passkey asks for a fresh code, so a stolen inbox cannot strip the second factor. Each code is single-use — if a prompt is submitted twice, use the fresh code your app shows next, not the previous one.",
    ],
  },
  {
    id: "trusted-devices",
    title: "Trusted devices",
    body: [
      "When 2FA is enabled you can tick “don't ask for a code on this device for 30 days” at login — that device is then trusted and skips the 2FA code (the password is still required).",
      "Profile → Security lists every trusted device (label, IP, first trusted, expiry) with a Revoke button per device, plus “revoke all / log out everywhere”.",
      "Revocation is immediate: the next login on a revoked device asks for a 2FA code again. Changing your password or disabling 2FA also clears all trusted devices.",
      "AI agents see and revoke the same list over MCP with list_trusted_devices, revoke_trusted_device and revoke_all_trusted_devices.",
    ],
  },
  {
    id: "login-sessions",
    title: "Sessions that need a fresh code",
    body: [
      "A session created through a magic link is marked as a magic session. With 2FA enabled, a sensitive action in such a session — changing the password, disabling 2FA, adding a passkey — asks for a fresh 2FA code.",
      "A normal password login and an account without 2FA are unaffected.",
    ],
  },
  {
    id: "passkeys",
    title: "Passkeys (sign in without a password)",
    body: [
      "A passkey lets you sign in with your face, fingerprint or device PIN instead of typing a password. Register one from Profile → Security → Register passkey: your browser asks your device to create it, and the passkey appears in the list with the date it was added.",
      "Once you have one, the login page offers a passkey button before you type anything, because signing in with a passkey starts from a list of your credentials rather than an email address. Press the button, approve the prompt, and you are in — nothing is typed.",
      "Each passkey is bound to one device or password manager (iCloud Keychain, Google Password Manager, 1Password, a security key), so add one per device you sign in from, and remove any you no longer use with the delete button on its row. Deleting a passkey stops it working immediately, and you can always sign in the ordinary way instead.",
      "If a passkey was added before this flow was fixed it may have been stored as a non-discoverable credential, which the login button cannot use. Delete that row and register a new passkey, which is made discoverable so the button finds it — the login page's button is the one that is refused, and re-registering is the only recovery.",
      "Adding a passkey from a magic-link session asks for a fresh 2FA code, the same as changing your password or disabling 2FA.",
    ],
  },
];

export default function SecurityDocsPage() {
  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-bold tracking-tight text-slate-900">Trusted devices &amp; 2FA</h1>
      <p className="mt-3 text-sm leading-relaxed text-slate-600">
        What a trusted device is, how long it lasts, how to see and revoke every one of them, and
        which sessions still need a fresh code.
      </p>

      {SECTIONS.map((s) => (
        <section key={s.id} id={s.id} className="mt-10 scroll-mt-24">
          <h2 className="text-lg font-semibold text-slate-900">{s.title}</h2>
          {s.body.map((p, i) => (
            <p key={i} className="mt-3 text-sm leading-6 text-slate-600">
              {p}
            </p>
          ))}
        </section>
      ))}

      <p className="mt-10 text-sm leading-relaxed text-slate-600">
        See the{" "}
        <a href="/docs/mcp" className="text-[var(--jf-primary)] underline underline-offset-2">
          MCP tool reference
        </a>{" "}
        for the exact tool signatures, and Profile → Security in the dashboard for the list itself.
      </p>
    </div>
  );
}
