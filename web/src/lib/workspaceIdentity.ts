// Workspace identity limits, in ONE place so the two forms that carry a
// workspace name cannot drift apart.
//
// WHY THIS MODULE EXISTS. The name field on Settings → Workspace (rename) and
// the one inside the topbar switcher (create) are the same value governed by the
// same server rule, but the create field shipped with no cap at all while the
// rename field carried `maxLength={120}`. A merchant could type an unbounded
// name into the create form and only discover the server's refusal from a failed
// request — the create/rename asymmetry this constant removes.
//
// THE UNIT MATTERS AND IT IS NOT BYTES. `maxLength` on an <input> is a count of
// UTF-16 code units. The Go rule (dbstore.WorkspaceNameMaxChars /
// ValidateWorkspaceName) counts CHARACTERS (utf8.RuneCountInString). For every
// character outside a surrogate pair — i.e. anything a merchant types, including
// accented Latin, CJK, Tamil and Arabic — one character is one code unit, so the
// two agree. The server used to count BYTES, which made it stricter than the
// field the user was typing into: 120 accented characters showed as 120/120 in
// the field and were refused by the API (240 bytes).
//
// The value is held here and asserted against the Go constant by
// workspaceIdentity.test.ts, so a change on one side fails the web build rather
// than producing a form that accepts what the server refuses.
export const WORKSPACE_NAME_MAX_CHARS = 120;
