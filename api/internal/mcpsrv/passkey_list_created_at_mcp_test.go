package mcpsrv

// t_ef0080c6 — MCP `list_passkeys` had no guard at all.
//
// THE DEFECT THIS GUARDS (card t_96c7c227, fixed in v1.21.210): `passkeyOut`
// declared `created_at` with `omitempty` and the handler never set it — the
// store behind it read no timestamp — so every item the tool returned simply
// had no timestamp in it. Dropping `omitempty` made a regression a WRONG VALUE
// rather than a VANISHED KEY, and this test is the arm that says so: nothing
// else in the tree reads `list_passkeys` from a test (`git grep -l list_passkeys
// -- '*_test.go'` found none before this file), so without it the tool could
// lose the field again and every gate would stay green.
//
// WHAT IS ASSERTED, and why each arm is needed:
//
//   - The call goes through the real MCP PROTOCOL — an in-memory transport, a
//     registered server, `registerTools`, `tools/call` — not the handler
//     function. That covers the registration in tools.go, the output schema the
//     go-sdk reflects out of `*listOutput[passkeyOut]`, and the JSON a client
//     actually receives. A handler-level call would miss all three.
//
//   - The result is decoded as raw messages, per item, rather than into a typed
//     struct: "the key is absent", "the value is null" and "the value is empty"
//     all unmarshal into the same zero string, and `omitempty` produces exactly
//     the first of those.
//
//   - The fixture row is BACKDATED seven days before the call. `passkeys.
//     created_at` is `DEFAULT now()`, so a row stored a moment ago sits in the
//     same second as a handler that formatted the clock at read time and the
//     equality arm would pass for the regression it exists to catch. That is
//     not hypothetical: the REST twin of this guard (httpapi/
//     passkey_list_created_at_test.go) was measured GREEN against a re-now()
//     handler at v1.21.210, and only a +48h control reddened it. With a
//     week-old row, the arm fails for a re-now() handler AND for a zero time
//     (0001-01-01T00:00:00Z, the "column never selected" shape) and passes only
//     for the row's own value.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// TestMCPListPasskeysCarriesTheStoredCreatedAt is the regression guard for the
// MCP half of t_96c7c227.
//
// Neutralise the fix — drop `CreatedAt` from the passkeyOut composite literal
// in listPasskeys, or put `,omitempty` back on the field, or stop selecting the
// column in PasskeyStore.ListCreatedAt — and this test goes red on the absent
// key; the REST guard reds as well, from the same store, and nothing else in
// the tree notices.
func TestMCPListPasskeysCarriesTheStoredCreatedAt(t *testing.T) {
	ctx := context.Background()
	if strings.TrimSpace(os.Getenv("JOMFORM_TEST_DATABASE_URL")) == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP list_passkeys created_at test")
	}
	pool := testdb.Open(t)

	// A user with an account: the tool resolves the caller through the
	// identity, and the account keeps the fixture shaped like a real caller
	// rather than an accountless one no session produces.
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcp-passkey-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'mcp-passkey-' || md5(random()::text), 'mcp-passkey-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// A stored credential, through the product's own store, with the shape a
	// registration writes: a 32-byte credential id, a COSE public key, the
	// flags of an attested discoverable credential and a 16-byte AAGUID.
	credID := make([]byte, 32)
	if _, err := rand.Read(credID); err != nil {
		t.Fatalf("generate credential id: %v", err)
	}
	passkeys := dbstore.NewPasskeyStore(pool)
	if err := passkeys.Save(ctx, userID, webauthn.Credential{
		ID:        credID,
		PublicKey: []byte{0xa5, 0x01, 0x02, 0x03, 0x26, 0x20, 0x01, 0x21, 0x58, 0x20},
		Flags: webauthn.NewCredentialFlags(protocol.FlagUserPresent |
			protocol.FlagAttestedCredentialData | protocol.FlagBackupEligible),
		Authenticator: webauthn.Authenticator{AAGUID: make([]byte, 16)},
	}); err != nil {
		t.Fatalf("store the fixture passkey: %v", err)
	}
	t.Cleanup(func() {
		// The test database is shared and long-lived; leave only the fixture
		// user behind, as the sibling MCP tests do.
		_, _ = pool.Exec(context.Background(), `DELETE FROM passkeys WHERE id=$1`, credID)
	})

	// ── the timestamp under test ────────────────────────────────────────────
	// Backdate the row so it cannot equal a clock read at call time. Without
	// this the equality arm below is vacuous — see the file comment.
	if _, err := pool.Exec(ctx, `UPDATE passkeys SET created_at = now() - interval '7 days' WHERE id=$1`, credID); err != nil {
		t.Fatalf("backdate the fixture row: %v", err)
	}
	var storedCreatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at FROM passkeys WHERE id=$1`, credID).
		Scan(&storedCreatedAt); err != nil {
		t.Fatalf("read the fixture row's created_at: %v", err)
	}
	// Guard the guard: a fixture that stopped being backdated would make the
	// arm pass for a re-now() handler again, silently.
	if age := time.Since(storedCreatedAt); age < 6*24*time.Hour {
		t.Fatalf("the fixture row's created_at is %s (%.1fh old), not the backdated value — the equality arm "+
			"below cannot distinguish the row's timestamp from a re-read clock",
			storedCreatedAt.UTC().Format(time.RFC3339), age.Hours())
	}

	// ── the call, over the real protocol ─────────────────────────────────────
	srv := &Server{
		mcp: mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{
			Passkeys: passkeys,
			Accounts: dbstore.NewAccountStore(pool),
		},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "passkey-list-test", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	// The server session's context is what a real transport derives the request
	// context from, so the authenticated principal rides on it.
	authCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, UserID: userID, Scopes: []string{"readwrite"}})
	serverSession, err := srv.mcp.Connect(authCtx, sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(ctx, cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "list_passkeys"})
	if err != nil {
		t.Fatalf("tools/call list_passkeys: %v", err)
	}
	if res.IsError {
		t.Fatalf("tools/call list_passkeys returned an error result: %s", toolResultText(t, res))
	}

	wantID := base64URLMCP(credID)
	var envelope struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(toolResultText(t, res)), &envelope); err != nil {
		t.Fatalf("decode the list_passkeys body: %v (%s)", err, toolResultText(t, res))
	}
	items := envelope.Items

	var mine map[string]json.RawMessage
	ids := make([]string, 0, len(items))
	for _, it := range items {
		var id string
		if raw, ok := it["id"]; ok {
			_ = json.Unmarshal(raw, &id)
		}
		ids = append(ids, id)
		if id == wantID {
			mine = it
		}
	}
	if mine == nil {
		t.Fatalf("the stored credential %s is not in list_passkeys %v (%s)", wantID, ids, toolResultText(t, res))
	}

	// ── arm 1: the key is PRESENT ────────────────────────────────────────────
	raw, ok := mine["created_at"]
	if !ok {
		t.Fatalf("the item carries no `created_at` key at all (%s) — the tool advertises the field and must "+
			"return it on every item; a key that is absent is what `omitempty` on passkeyOut.CreatedAt "+
			"produces, and no typed decode of the result can tell that from an empty value",
			toolResultText(t, res))
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("`created_at` is not a JSON string: %s", raw)
	}

	// ── arm 2: non-empty and RFC 3339 ────────────────────────────────────────
	if got == "" {
		t.Fatalf("`created_at` is empty for a stored passkey (%s), but the row's column holds %s — the tool "+
			"reports a timestamp it never reads, which is the defect of card t_96c7c227",
			toolResultText(t, res), storedCreatedAt.UTC().Format(time.RFC3339))
	}
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("`created_at` is %q, which is not RFC 3339: %v", got, err)
	}

	// ── arm 3: it EQUALS the row's column ────────────────────────────────────
	if !parsed.Equal(storedCreatedAt.UTC().Truncate(time.Second)) {
		t.Fatalf("`created_at` is %q, but the credential's row holds %s — list_passkeys must report the row's "+
			"own timestamp. The row is backdated seven days, so a value near the current clock means the "+
			"handler formatted time.Now() instead of the column, and an all-zero value means it formatted "+
			"nothing", got, storedCreatedAt.UTC().Format(time.RFC3339))
	}

	// The structured payload is what a client validating the tool's outputSchema
	// reads; it must not disagree with the text the client also gets. The go-sdk
	// always fills it for a typed Out (server.go: "avoid a second marshal over
	// the wire"), so a result without an object here is itself a contract break
	// rather than something to skip past.
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("the tool result carries no structuredContent object (%T), but list_passkeys declares an "+
			"object output schema", res.StructuredContent)
	}
	scItems, _ := sc["items"].([]any)
	scFound := false
	for _, it := range scItems {
		obj, _ := it.(map[string]any)
		if obj == nil {
			continue
		}
		if objID, _ := obj["id"].(string); objID != wantID {
			continue
		}
		scFound = true
		scGot, present := obj["created_at"]
		if !present {
			t.Fatalf("structuredContent's item for %s has no `created_at` (%v)", wantID, obj)
		}
		if s, _ := scGot.(string); s != got {
			t.Fatalf("structuredContent reports created_at %q but the text content reports %q", s, got)
		}
	}
	if !scFound {
		t.Fatalf("structuredContent does not list the stored credential %s (%v)", wantID, sc)
	}
}
