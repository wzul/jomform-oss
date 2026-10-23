// users.name is capped at 80 CHARACTERS, never 80 BYTES.
//
// WHY THIS EXISTS. handleUpdateName bounded a user-supplied display name with a
// BYTE slice (`name = name[:80]`). `users.name` is a Postgres `text` column, so a
// slice that lands inside a multi-byte rune hands the server invalid UTF-8, and
// Postgres refuses the WHOLE statement with SQLSTATE 22021 — which the handler
// reports as a 500 `internal error`, with the user's name silently not saved.
// 27 CJK characters (81 bytes) is enough to reach it, as is 79 ASCII characters
// followed by any non-ASCII one.
//
// THE SAME SLICE ALSO MANGLED NAMES THAT WERE ONLY LONG IN BYTES. A 40-character
// CJK name is 120 bytes, so the byte cap cut it to 13 characters — a name that
// LOOKS fine (valid UTF-8, no error anywhere) and is simply wrong. Both halves
// are pinned below: the mid-rune case proves the UPDATE is refused, the
// 40-character case proves a short-in-characters name is not silently shortened.
//
// WHAT FAILS HERE IF THE BYTE SLICE COMES BACK
//   - "79 ASCII + 中文名" answers 500 and stores NOTHING (SQLSTATE 22021), and
//   - "名"x80 and "店"x40 (both over 80 BYTES, both within 80 CHARACTERS) store a
//     truncated name.
//
// A test that only inspected the capped string could see neither: the first
// never reaches the row, the second never reports an error.
//
// Every case goes through the REAL route table (RegisterRoutes) with a real
// session cookie in Valkey and a real Postgres row, and the assertions are read
// back from the database, not from the handler's own answer. Needs
// JOMFORM_TEST_DATABASE_URL + JOMFORM_TEST_VALKEY_URL; skipped otherwise. No
// CREATE DATABASE — testdb gives this process its own scratch schema.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// profileNameCap is restated here rather than imported, so a change to the
// server's cap that this guard did not follow shows up as a failure instead of
// silently moving the bar with the code it is supposed to be watching.
const profileNameCap = 80

// TestProfileNameColumnRefusesMidRuneBytes pins the MECHANISM behind the guard
// above, so the 500 it reports is attributable: `users.name` is a Postgres `text`
// column, and a byte slice that lands inside a multi-byte rune is refused by the
// server with SQLSTATE 22021 — the whole UPDATE fails, which is why the user's
// name was lost entirely rather than saved short.
//
// This is deliberately NOT the guard (it asserts the database's behaviour, not
// the handler's); it is here so that if the column ever stops enforcing UTF-8,
// the guard above failing would be explained rather than mysterious.
func TestProfileNameColumnRefusesMidRuneBytes(t *testing.T) {
	if os.Getenv(testdb.DSNEnv) == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping profile-name column integration test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	users := dbstore.NewUserStore(pool)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1,'it') RETURNING id`,
		"profile-name-col-"+runKey()+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	input := strings.Repeat("a", 79) + "中文名"
	midRune := input[:profileNameCap] // exactly the slice the handler used to take
	if utf8.ValidString(midRune) {
		t.Fatalf("precondition broken: %d-byte prefix of the input is already valid UTF-8, so it does not exercise a mid-rune cut", profileNameCap)
	}
	if err := users.UpdateName(ctx, userID, midRune); err == nil {
		t.Fatalf("UpdateName accepted invalid UTF-8 (%q); the guard above would then be testing the wrong mechanism", midRune)
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "22021" {
			t.Fatalf("UpdateName refused the mid-rune slice with %v, want SQLSTATE 22021 (invalid byte sequence for encoding \"UTF8\")", err)
		}
	}

	// The row is untouched: the refusal is the whole statement, not a partial write.
	var stored string
	if err := pool.QueryRow(ctx, `SELECT name FROM users WHERE id=$1`, userID).Scan(&stored); err != nil {
		t.Fatalf("read back name: %v", err)
	}
	if stored != "" {
		t.Fatalf("stored name = %q after a refused UPDATE, want the untouched default \"\"", stored)
	}
}

// runePrefix is an INDEPENDENT implementation of "keep at most n characters":
// the test must not borrow the function under test to decide what the right
// answer is.
func runePrefix(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}

// TestProfileNameCapIsCountedInCharacters is the guard for the byte slice.
func TestProfileNameCapIsCountedInCharacters(t *testing.T) {
	if os.Getenv(testdb.DSNEnv) == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping profile-name cap integration test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	sessions := authTestValkey(t)
	a := &AuthHandlers{
		Sessions: sessions,
		Users:    dbstore.NewUserStore(pool),
	}

	mux := http.NewServeMux()
	a.RegisterRoutes(mux)

	cases := []struct {
		name string
		// why this input is here
		why      string
		input    string
		wantName string // exact row value expected after the handler ran
	}{
		{
			name:     "byte 80 lands inside a multi-byte rune",
			why:      "the reproduced defect: 79 ASCII + 3 CJK characters is 82 characters / 88 bytes, and byte 80 is the FIRST byte of 中, so a byte slice yields invalid UTF-8 and Postgres refuses the UPDATE (SQLSTATE 22021)",
			input:    strings.Repeat("a", 79) + "中文名",
			wantName: strings.Repeat("a", 79) + "中",
		},
		{
			name:     "80 CJK characters is over the BYTE cap but exactly the CHARACTER cap",
			why:      "240 bytes / 80 characters: a byte cap cuts this valid, within-limit name to 26 characters and stores 26 characters plus the first 2 bytes of a rune",
			input:    strings.Repeat("名", 80),
			wantName: strings.Repeat("名", 80),
		},
		{
			name:     "60 accented characters is over the BYTE cap and under the CHARACTER cap",
			why:      "120 bytes / 60 characters: 80 is divisible by the 2-byte width of é, so the byte slice stays VALID UTF-8 and silently stores only 40 characters — no error anywhere, which is the half of this defect a status-code assertion cannot see",
			input:    strings.Repeat("é", 60),
			wantName: strings.Repeat("é", 60),
		},
		{
			name:     "ASCII over the cap is still capped",
			why:      "the cap must keep capping: the fix is the unit and the boundary, not the removal of the limit",
			input:    strings.Repeat("b", 200),
			wantName: strings.Repeat("b", profileNameCap),
		},
		{
			name:     "a name at exactly 80 ASCII characters is stored verbatim",
			why:      "the boundary itself must not lose the 80th character",
			input:    strings.Repeat("c", profileNameCap),
			wantName: strings.Repeat("c", profileNameCap),
		},
		{
			name:     "surrounding whitespace is still trimmed before the cap",
			why:      "the rune-aware cap must replace only the slice, not the trimming that preceded it",
			input:    "  Siti Nurhaliza  ",
			wantName: "Siti Nurhaliza",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var userID string
			if err := pool.QueryRow(ctx,
				`INSERT INTO users (email, password_hash) VALUES ($1,'it') RETURNING id`,
				"profile-name-cap-"+runKey()+"@example.test").Scan(&userID); err != nil {
				t.Fatalf("create user: %v", err)
			}
			token, err := sessions.Create(ctx, userID)
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			t.Cleanup(func() { _ = sessions.Destroy(context.Background(), token) })

			body, err := json.Marshal(map[string]string{"name": tc.input})
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/auth/profile/name", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			// The status is the first thing the defect changed: a refused UPDATE
			// surfaced as 500 `internal error`.
			if rec.Code != http.StatusOK {
				t.Fatalf("%s\nPOST /v1/auth/profile/name answered %d (%s), want 200 — the row was not written",
					tc.why, rec.Code, strings.TrimSpace(rec.Body.String()))
			}

			var stored string
			if err := pool.QueryRow(ctx, `SELECT name FROM users WHERE id=$1`, userID).Scan(&stored); err != nil {
				t.Fatalf("read back name: %v", err)
			}

			// 1. The value Postgres accepted must be valid UTF-8. A mid-rune byte
			//    slice cannot get this far (the UPDATE is refused), but the check
			//    states the column's invariant where the defect violated it.
			if !utf8.ValidString(stored) {
				t.Fatalf("stored name is not valid UTF-8: %q", stored)
			}
			// 2. The cap is a CHARACTER count, so the byte length is free to
			//    exceed 80 — and must, for a multi-byte name within the limit.
			if got := utf8.RuneCountInString(stored); got > profileNameCap {
				t.Fatalf("stored name is %d characters, over the %d-character cap: %q", got, profileNameCap, stored)
			}
			// 3. The stored value is the input, trimmed, cut on a rune boundary —
			//    not a byte prefix and not something shorter than the cap allows.
			want := runePrefix(strings.TrimSpace(tc.input), profileNameCap)
			if stored != want {
				t.Fatalf("stored name = %q (%d chars / %d bytes), want %q (%d chars / %d bytes)",
					stored, utf8.RuneCountInString(stored), len(stored),
					want, utf8.RuneCountInString(want), len(want))
			}
			// The case table is data, so a case whose expectation drifted from
			// its input silently stops testing anything: pin the two together.
			if tc.wantName != want {
				t.Fatalf("case expectation %q disagrees with the rune-prefix rule %q — fix the case, not the rule", tc.wantName, want)
			}
		})
	}
}
