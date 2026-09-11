// Session value codec tests (issues #69 + #458).
//
// The session value is a plain ":"-joined string persisted in Valkey, so every
// historical format must keep parsing: a running deployment must never log out
// its users when the codec gains a component. These tests are pure (no Valkey)
// and cover the v4 magic flag plus the legacy v1/v2/v3 shapes.
package auth

import (
	"testing"
	"time"
)

func TestSessionValueRoundTripMagic(t *testing.T) {
	issued := time.Date(2026, 9, 9, 12, 0, 0, 123456789, time.UTC)

	cases := []struct {
		name     string
		remember bool
		magic    bool
	}{
		{"plain", false, false},
		{"remembered", true, false},
		{"magic", false, true},
		{"magic+remembered", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := encodeValue("user-123", issued, tc.remember, tc.magic)
			uid, gotIssued, gotRemember, gotMagic := parseValue(v)
			if uid != "user-123" {
				t.Errorf("userID = %q, want user-123", uid)
			}
			if !gotIssued.Equal(issued) {
				t.Errorf("issued = %v, want %v", gotIssued, issued)
			}
			if gotRemember != tc.remember {
				t.Errorf("remember = %v, want %v", gotRemember, tc.remember)
			}
			if gotMagic != tc.magic {
				t.Errorf("magic = %v, want %v", gotMagic, tc.magic)
			}
		})
	}
}

// TestSessionValueV4Shape pins the on-the-wire format so a future refactor
// can't silently change the persisted representation.
func TestSessionValueV4Shape(t *testing.T) {
	issued := time.Unix(0, 1699999999000000000).UTC()
	if got := encodeValue("u1", issued, true, true); got != "u1:1699999999000000000:1:1" {
		t.Errorf("v4 remembered+magic = %q", got)
	}
	if got := encodeValue("u1", issued, false, false); got != "u1:1699999999000000000:0:0" {
		t.Errorf("v4 plain = %q", got)
	}
}

// TestSessionValueLegacyFormats: v1 (bare id), v2 (id:issued), v3
// (id:issued:remember) must all still parse, with magic defaulting to false —
// the safe default, since an unknown login method must never be treated as a
// magic session.
func TestSessionValueLegacyFormats(t *testing.T) {
	cases := []struct {
		name         string
		value        string
		wantUser     string
		wantIssuedNs int64 // 0 = expect zero time
		wantRemember bool
	}{
		{"v1 bare userID", "user-legacy", "user-legacy", 0, false},
		{"v2 issued only", "user-v2:1699999999000000000", "user-v2", 1699999999000000000, false},
		{"v3 remember=0", "user-v3:1699999999000000000:0", "user-v3", 1699999999000000000, false},
		{"v3 remember=1", "user-v3b:1699999999000000000:1", "user-v3b", 1699999999000000000, true},
		{"v3 malformed issued", "user-bad:notanumber:1", "user-bad", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uid, issued, remember, magic := parseValue(tc.value)
			if uid != tc.wantUser {
				t.Errorf("userID = %q, want %q", uid, tc.wantUser)
			}
			if tc.wantIssuedNs == 0 {
				if !issued.IsZero() {
					t.Errorf("issued = %v, want zero", issued)
				}
			} else if issued.UnixNano() != tc.wantIssuedNs {
				t.Errorf("issued ns = %d, want %d", issued.UnixNano(), tc.wantIssuedNs)
			}
			if remember != tc.wantRemember {
				t.Errorf("remember = %v, want %v", remember, tc.wantRemember)
			}
			if magic {
				t.Error("legacy value must never parse as a magic session")
			}
		})
	}
}

// TestSessionValueMagicFlagNotConfusedByTrailingParts: only the 4th component
// is the magic flag, and a garbage 4th component is not magic.
func TestSessionValueMagicFlagNotConfusedByTrailingParts(t *testing.T) {
	if _, _, _, magic := parseValue("u:1699999999000000000:0:0"); magic {
		t.Error("explicit magic=0 parsed as magic")
	}
	if _, _, _, magic := parseValue("u:1699999999000000000:0:garbage"); magic {
		t.Error("garbage magic component parsed as magic")
	}
	if _, _, _, magic := parseValue("u:1699999999000000000:0:1"); !magic {
		t.Error("explicit magic=1 not parsed as magic")
	}
}
