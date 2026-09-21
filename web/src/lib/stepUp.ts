// Step-up rules for the Profile → Security actions (issue #458).
//
// A magic-link session belongs to a user who proved only that they can read an
// inbox. When that user also has 2FA enabled, the sensitive actions (change
// password / disable 2FA / add a passkey) additionally need a FRESH 6-digit code
// from their authenticator. Everyone else is unaffected:
//
//   - a magic session for a user WITHOUT 2FA proves a password instead;
//   - a normal (password / social / passkey) session for a 2FA user already
//     proved a factor at login.
//
// This module is pure (no React, no DOM) so it can be imported by the page AND
// by the node test runner, which cannot load a .tsx module. The page must not
// restate the condition inline: a second copy is how the two drift apart.

export type StepUpSession = {
  magic_session?: boolean;
} | null | undefined;

export type StepUpProfile = {
  totp_enabled?: boolean;
} | null | undefined;

// stepUpRequired reports whether a fresh 2FA code must be supplied for the
// given session + profile before a sensitive action runs.
export function stepUpRequired(
  session: StepUpSession,
  profile: StepUpProfile,
): boolean {
  return Boolean(session?.magic_session && profile?.totp_enabled);
}

// isValidStepUpCode normalises a prompted code: a non-empty, trimmed value.
// Both prompts (the pre-emptive one and the retry after a 401 challenge) use
// it, so an empty prompt is refused identically on both paths.
export function isValidStepUpCode(raw: string | null): boolean {
  return typeof raw === "string" && raw.trim() !== "";
}
