// The API's session cookie name, shared with the web app.
//
// WHY THIS IS A SHARED CONSTANT AND NOT A LITERAL: the home page decides what
// to render server-side by looking for this cookie, so the name is now load
// bearing on BOTH sides of the wire. The API writes it (`sessionCookie` in
// api/internal/httpapi/auth.go) and the web app reads it; if the two drift, the
// failure is silent in the worst possible direction — a signed-in user becomes
// "anonymous" to the home page and is served the marketing landing page instead
// of their dashboard, with no error anywhere. homeServerContent.test.ts reads
// the Go constant and fails if the two disagree.
//
// The cookie is HttpOnly and is set only by the API on login (and cleared on
// logout with Max-Age=0, which removes it), so its PRESENCE is a trustworthy
// server-side signal that this browser has been through a login. Its VALIDITY
// is not: an expired or revoked session still carries the cookie, which is why
// the page still hands a cookie-bearing visitor to the client dashboard bundle
// — that 401 handling is unchanged, and it is what flips such a visitor to the
// landing page.
export const SESSION_COOKIE = "jf_session";
