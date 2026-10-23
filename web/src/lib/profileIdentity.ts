// The profile display-name cap — one value, written in two languages.
//
// WHY THIS FILE EXISTS. The server capped the name with a BYTE slice
// (`name[:80]` in api/internal/httpapi/profile.go). A cut that lands inside a
// multi-byte rune hands Postgres invalid UTF-8, the UPDATE is refused
// (SQLSTATE 22021), and the handler answered 500 "internal error" — so a name
// like 78 ASCII characters followed by a CJK character was NEVER SAVED, and the
// user was told nothing useful. 80 characters of non-ASCII text is 160–320
// bytes, so the byte cap fired long before the 80-character one would.
//
// The Go side now caps at 80 CHARACTERS on a rune boundary
// (`profileNameMaxChars` + textutil.Truncate in api/internal/httpapi/profile.go),
// and this constant is the field's half of the same rule: the input refuses to
// accept more than the server can store, so the truncation path is only ever
// reached by an API client, never by a person typing.
//
// THE UNIT MATTERS. `maxLength` on an <input> counts UTF-16 code units, while Go
// counts runes. For every character outside a surrogate pair (accented Latin,
// CJK, Tamil, Arabic) one character is one code unit, so the two agree exactly.
// An astral character (an emoji) is 1 rune in Go but 2 code units here, so this
// field is STRICTER than the server for emoji — the safe direction: the field can
// never accept a name the server would have to cut.
//
// The value is asserted against the Go constant by profileIdentity.test.ts, so a
// change on one side fails the web build instead of shipping a field that
// accepts what the server truncates.
export const PROFILE_NAME_MAX_CHARS = 80;

// normalizeProfileName mirrors exactly what the save handler stores:
// `textutil.Truncate(strings.TrimSpace(req.Name), profileNameMaxChars)`.
//
// The field needs it for one visible reason: after a successful save the page
// re-reads the profile, but `load()` keeps whatever is already in the input
// (`setName((cur) => cur || sessionUser.name || "")`), so the field went on
// showing the raw text the user typed while the database held the trimmed,
// truncated value — the mismatch only healed on the next mount. Applying the
// server's rule to the field on success makes what is displayed the thing that
// was stored. (maxLength already makes the LENGTH part unreachable from the
// field for typed or pasted input; this covers surrounding whitespace too.)
export function normalizeProfileName(raw: string): string {
  // Array.from splits by CODE POINT, the same unit Go counts a rune in, so the
  // cut lands where the server's cut lands instead of splitting a surrogate pair.
  return Array.from(raw.trim()).slice(0, PROFILE_NAME_MAX_CHARS).join("");
}
