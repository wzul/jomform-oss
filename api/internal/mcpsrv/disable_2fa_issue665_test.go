package mcpsrv

// disable_2fa: the documented inputs, the tool's inputSchema, and what the tool
// actually accepts are ONE contract, asserted in both directions.
//
// WHY THIS FILE EXISTS. PR #666 (the fix for the /profile 2FA-disable control)
// changed the sentence for `disable_2fa` on web/src/app/docs/mcp/page.tsx to say
// the tool accepts a current 2FA code (`totp_code`), while the MCP tool's
// inputSchema was `{password}` only with `additionalProperties:false`. Nothing
// failed: mcp_parity_test.go (issue #513) only proves a checklist tool is
// NAMED on a docs page, and it says so — it is a text-presence check, not an
// input-contract check. The whole suite stayed green while the docs page told
// agents to send a field the tool rejected with
//   validating "arguments": unexpected additional properties ["totp_code"]
//
// So the sentence was either made true (give the tool the field) or corrected
// (say password only). Making it true is the right call here: an account that
// joined through an emailed link has a random-UUID password nobody has seen, so
// `password` alone made 2FA impossible to turn off from MCP for exactly the
// accounts the dashboard fix was for.
//
// The schema is the whole contract, enforced before the handler runs:
//
//	{"totp_code":"123456"}         -> allowed (single-use, consumed)
//	{"password":"…"}               -> allowed (password path, unchanged)
//	{"password":"…","totp_code":…} -> refused (oneOf: validated against both)
//	{}                             -> refused (validates against neither)
//	{"nope":"x"}                   -> refused (additionalProperties:false)
//
// WHAT THIS FILE PINS
//
//	TestDisable2FAInputSchemaMatchesDocs            (no DB)  — tools/list declares
//	  both fields, `oneOf` requires exactly one of them, and the docs page and
//	  the tool Description name each declared field. Mutating either side red.
//	TestDisable2FATotpCodeDisables                (real DB) — a REAL TOTP code in
//	  totp_code over the real MCP transport disables 2FA and clears the secret
//	  column. This is the path the card says was impossible.
//	TestDisable2FACodeIsSingleUseOverMCP          (real DB) — replaying the same
//	  code is refused and 2FA stays on; the second call must not be a second
//	  successful disable. (#528 rule, now on this surface too.)
//	TestDisable2FAReplayDoesNotFallBackToPassword (real DB) — the sharpest one:
//	  a replayed code sent with the CORRECT account password is still refused. A
//	  rejected code must never degrade into the password path.
//	TestDisable2FAWithoutEitherProofIsRefused     (real DB) — everything that is
//	  not exactly one valid proof is refused AND the account keeps its 2FA. A
//	  "disable everything" implementation cannot satisfy this.
//	TestDisable2FAIsRefusedForAReadonlyKey        (real DB) — the scope half
//	  (issue #645): every positive test uses a readwrite key, so without this a
//	  fix that dropped the write gate would still look green.
//
// The behavioural tests skip without JOMFORM_TEST_DATABASE_URL, like every other
// DB-backed test in this package; the schema test always runs.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// The docs entry for disable_2fa is located through docsToolRowFor
// (docs_surface_test.go), which reads the whole /docs/mcp surface — the hub plus
// the four task pages plus the tool-list module they render. A single hard-coded
// page was correct only while /docs/mcp was one page; after the split the row
// lives in web/src/lib/mcpToolGroups.ts and a page-path constant made this guard
// red against a correct tree (card t_ee4114af).

// currentTOTPCode generates the code an authenticator app would show right now.
func currentTOTPCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, time.Now(), totp.ValidateOpts{
		Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}
	return code
}

// listTool returns one tool definition as an MCP client actually receives it —
// over tools/list, not read off the Go struct the server builds.
func listTool(t *testing.T, name string) *mcp.Tool {
	t.Helper()
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "disable2fa-test", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.mcp.Connect(context.Background(), sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(context.Background(), cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	var found *mcp.Tool
	cursor := ""
	for {
		res, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		for _, tl := range res.Tools {
			if tl.Name == name {
				found = tl
			}
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if found == nil {
		t.Fatalf("%s is not registered — every assertion in this file would be vacuous", name)
	}
	return found
}

// disable2faToolSchema reads the tool's inputSchema off the real tools/list.
func disable2faToolSchema(t *testing.T) map[string]any {
	t.Helper()
	tool := listTool(t, "disable_2fa")
	if tool.InputSchema == nil {
		t.Fatal("disable_2fa has no inputSchema")
	}
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal inputSchema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("inputSchema is not a JSON object: %v (%s)", err, raw)
	}
	if schema["type"] != "object" {
		t.Fatalf("inputSchema type = %v, want object; raw=%s", schema["type"], raw)
	}
	// Log the schema as an MCP client receives it, so a failure report (and a
	// reviewer's own run) shows the real contract rather than a paraphrase.
	t.Logf("disable_2fa inputSchema: %s", raw)
	return schema
}

// TestDisable2FAInputSchemaMatchesDocs is the guard for the defect this card was
// filed for. It would have caught PR #666 as shipped: the docs sentence promised
// totp_code while the schema declared only password.
//
// The expectations are spelled out here rather than read from the Go struct,
// because a guard that derives its expectation from the code under test cannot
// catch that code changing.
func TestDisable2FAInputSchemaMatchesDocs(t *testing.T) {
	schema := disable2faToolSchema(t)

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("inputSchema has no properties object: %v", schema)
	}
	for _, want := range []string{"password", "totp_code"} {
		if _, ok := props[want]; !ok {
			t.Errorf("inputSchema is missing property %q (%v) — a client cannot send it, "+
				"and the docs page tells agents to", want, props)
		}
	}
	if len(props) != 2 {
		t.Errorf("inputSchema declares %d properties (%v), want exactly password + totp_code — "+
			"an extra field is a new contract and needs its own docs and test", len(props), props)
	}

	// The contract is "exactly one proof". `required` cannot say that (a field
	// is required or omitted, nothing between), so it must stay out of the way
	// and oneOf must carry the rule.
	if req, _ := schema["required"].([]any); len(req) > 0 {
		t.Errorf("inputSchema has a top-level required %v — the either/or rule lives in oneOf; "+
			"a required field here would reject a valid totp_code-only call (the reported defect)", req)
	}
	if schema["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v, want false — the declared set is the whole contract, "+
			"so a misspelled argument must be a refusal", schema["additionalProperties"])
	}

	oneOf, ok := schema["oneOf"].([]any)
	if !ok || len(oneOf) != 2 {
		t.Fatalf("inputSchema oneOf = %v, want exactly two alternatives ({password} | {totp_code})", schema["oneOf"])
	}
	var got []string
	for _, alt := range oneOf {
		m, ok := alt.(map[string]any)
		if !ok {
			t.Fatalf("oneOf alternative is not an object: %v", alt)
		}
		req, _ := m["required"].([]any)
		var names []string
		for _, r := range req {
			s, _ := r.(string)
			names = append(names, s)
		}
		sort.Strings(names)
		got = append(got, strings.Join(names, ","))
	}
	sort.Strings(got)
	if strings.Join(got, " | ") != "password | totp_code" {
		t.Fatalf("oneOf alternatives require %v, want exactly [password] and [totp_code] — "+
			"without this a call with BOTH proofs (or with neither) is accepted", got)
	}

	// The prose side. Both the docs entry an agent reads and the tool Description
	// an agent is handed must name each declared field. The entry is located on
	// the whole /docs/mcp surface — after the split the rows live in the module
	// the pages render (card t_ee4114af) — and a missing or duplicated entry is a
	// FAILURE here, never a skip.
	entry := docsToolRowFor(t, "disable_2fa")
	line := entry.text
	desc := listTool(t, "disable_2fa").Description
	if desc == "" {
		t.Fatal("disable_2fa has an empty Description")
	}
	for _, field := range []string{"password", "totp_code"} {
		if !strings.Contains(line, field) {
			t.Errorf("the disable_2fa docs entry (%s) does not name %q, but the tool's inputSchema "+
				"declares it.\ndocs: %s", entry, field, strings.TrimSpace(line))
		}
		if !strings.Contains(desc, field) {
			t.Errorf("the disable_2fa Description does not name %q, but the inputSchema declares "+
				"it.\ndescription: %s", field, desc)
		}
	}
}

// ── behavioural half (needs a real Postgres) ────────────────────────────────

// disable2faFixture wires a readwrite API key onto a REAL user row with 2FA
// enabled, over the real MCP HTTP transport.
type disable2faFixture struct {
	httpSrv   string
	key       string
	userID    string
	accountID string
	secret    string
	users     *dbstore.UserStore
	pool      *pgxpool.Pool
}

// valkeySessionsForTest returns a REAL session store, or skips the test when the
// integration Valkey is absent.
//
// #713 made `disable_2fa` refuse the call when the server has no session store:
// the tool must revoke the account's trusted devices (a set the session store
// owns in Valkey), and clearing the factor while leaving those 30-day
// exemptions alive is exactly the hole issue #63 exists to close. Every
// behavioural case in this file therefore needs a real store — with Sessions
// left nil it would be refused by that guard before its own proof is examined,
// i.e. it would pass or fail for a reason that is not the one under test.
func valkeySessionsForTest(t *testing.T) *auth.Sessions {
	t.Helper()
	valkeyURL := strings.TrimSpace(os.Getenv("JOMFORM_TEST_VALKEY_URL"))
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; disable_2fa revokes the account's " +
			"trusted devices (#713), and that set lives in Valkey")
	}
	sessions, err := auth.NewSessions(valkeyURL, testValkeyOptions()...)
	if err != nil {
		t.Fatalf("valkey sessions: %v", err)
	}
	t.Cleanup(func() { _ = sessions.Close() })
	return sessions
}

func newDisable2faFixture(t *testing.T, tag string) *disable2faFixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)
	accountID, userID := scopeTestAccount(t, pool, tag)

	users := dbstore.NewUserStore(pool)
	// A real secret, armed as if the user had confirmed it during setup.
	secret, _, err := auth.GenerateTOTPSecret("disable2fa-" + tag + "@example.test")
	if err != nil {
		t.Fatalf("generate TOTP secret: %v", err)
	}
	if err := users.EnableTOTP(ctx, userID, secret); err != nil {
		t.Fatalf("enable TOTP on the fixture user: %v", err)
	}

	keys := dbstore.NewAPIKeyStore(pool)
	rawKey := scopeTestKey(t, ctx, keys, accountID, tag+"-key", []string{dbstore.ScopeReadWrite})

	// #713: disable_2fa refuses the call when the server has no session store.
	// The tool must revoke the account's trusted devices (a set the session
	// store owns in Valkey) and clearing the factor while leaving those 30-day
	// exemptions alive is exactly the hole issue #63 exists to close. Wire a
	// REAL store here, or every behavioural case below is refused by that guard
	// before its own proof is ever examined.
	sessions := valkeySessionsForTest(t)

	srv := New(&Deps{
		Users: users, Keys: keys, Accounts: dbstore.NewAccountStore(pool),
		Forms: formsFor(pool), Sessions: sessions,
	}, nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(httpSrv.Close)

	return &disable2faFixture{
		httpSrv: httpSrv.URL, key: rawKey, userID: userID, accountID: accountID,
		secret: secret, users: users, pool: pool,
	}
}

// totpEnabled reads the users COLUMN — the effect, not the tool's reply. A tool
// that reported success without writing would pass an assertion on the reply.
func (f *disable2faFixture) totpEnabled(t *testing.T) (enabled bool, secretCleared bool) {
	t.Helper()
	var hasSecret bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT totp_enabled, COALESCE(totp_secret,'') <> '' FROM users WHERE id = $1`,
		f.userID).Scan(&enabled, &hasSecret); err != nil {
		t.Fatalf("read users row %s: %v", f.userID, err)
	}
	return enabled, !hasSecret
}

// TestDisable2FATotpCodeDisables is the card's reported case on the MCP surface:
// a current code in `totp_code` alone must turn 2FA off. On the pre-fix schema
// this call is rejected by validation ("unexpected additional properties"),
// which is exactly what the docs page promised would work.
func TestDisable2FATotpCodeDisables(t *testing.T) {
	requireTestDB(t)
	f := newDisable2faFixture(t, "mcptotp")

	res := mcpCall(t, f.httpSrv, f.key, "disable_2fa",
		map[string]any{"totp_code": currentTOTPCode(t, f.secret)})
	if res.IsError {
		t.Fatalf("tools/call disable_2fa {totp_code} was refused: %s", toolResultText(t, res))
	}
	if got, want := succeeded(t, res)["status"], "disabled"; got != want {
		t.Errorf("reply status = %v, want %v", got, want)
	}
	enabled, secretCleared := f.totpEnabled(t)
	if enabled {
		t.Error("users.totp_enabled is still true — the tool replied success without applying it")
	}
	if !secretCleared {
		t.Error("users.totp_secret is still set — DisableTOTP must clear the secret, not just the flag")
	}
}

// TestDisable2FACodeIsSingleUseOverMCP pins the #528 rule on this surface: the
// same code cannot be replayed. The second call is refused AND the account keeps
// its 2FA.
//
// It re-arms 2FA between the two calls with EnableTOTP (which writes the secret
// but leaves totp_last_step alone), so the replay assertion is about the
// consumed step and not merely about "2FA is already off".
func TestDisable2FACodeIsSingleUseOverMCP(t *testing.T) {
	requireTestDB(t)
	f := newDisable2faFixture(t, "mcpreplay")

	code := currentTOTPCode(t, f.secret)
	if res := mcpCall(t, f.httpSrv, f.key, "disable_2fa", map[string]any{"totp_code": code}); res.IsError {
		t.Fatalf("first disable was refused: %s", toolResultText(t, res))
	}
	if err := f.users.EnableTOTP(context.Background(), f.userID, f.secret); err != nil {
		t.Fatalf("re-enable TOTP: %v", err)
	}
	if enabled, _ := f.totpEnabled(t); !enabled {
		t.Fatal("fixture re-arm failed — 2FA is not enabled, so the replay assertion would be vacuous")
	}

	replay := mcpCall(t, f.httpSrv, f.key, "disable_2fa", map[string]any{"totp_code": code})
	if !replay.IsError {
		t.Fatalf("the SAME code disabled 2FA a second time (reply %s) — single-use (#528) is not "+
			"enforced on MCP", toolResultText(t, replay))
	}
	if enabled, _ := f.totpEnabled(t); !enabled {
		t.Error("2FA was turned off by the replayed code — the refusal came too late")
	}
}

// TestDisable2FAReplayDoesNotFallBackToPassword is the sharpest assertion here.
// A replayed (already consumed) code supplied TOGETHER WITH THE CORRECT ACCOUNT
// PASSWORD must still be refused.
//
// Note this call also has to get past the oneOf rule, which refuses both fields
// together — so it is refused by the CONTRACT before the handler. That is the
// right answer for two reasons: a code is not a step-up on top of a password on
// this surface, and a rejected code must never silently degrade into the
// password path (an implementation that "tried the password next" would let
// anyone who ever saw a code re-disable 2FA by adding the password).
//
// The password-only control at the end keeps this honest: it proves the password
// path itself still works, so the refusal above is about the combination and not
// a broken password check.
func TestDisable2FAReplayDoesNotFallBackToPassword(t *testing.T) {
	requireTestDB(t)
	f := newDisable2faFixture(t, "mcpreplaypw")

	const pw = "correct horse battery staple"
	hash, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE users SET password_hash = $2 WHERE id = $1`, f.userID, hash); err != nil {
		t.Fatalf("set password hash: %v", err)
	}

	code := currentTOTPCode(t, f.secret)
	if res := mcpCall(t, f.httpSrv, f.key, "disable_2fa", map[string]any{"totp_code": code}); res.IsError {
		t.Fatalf("first disable was refused: %s", toolResultText(t, res))
	}
	if err := f.users.EnableTOTP(context.Background(), f.userID, f.secret); err != nil {
		t.Fatalf("re-enable TOTP: %v", err)
	}

	replay := mcpCall(t, f.httpSrv, f.key, "disable_2fa",
		map[string]any{"totp_code": code, "password": pw})
	if !replay.IsError {
		t.Fatalf("a replayed code + the correct password disabled 2FA (reply %s)", toolResultText(t, replay))
	}
	if enabled, _ := f.totpEnabled(t); !enabled {
		t.Error("2FA was disabled by the replay+password combination")
	}

	// Control: the password path itself still works when NO code is sent.
	if res := mcpCall(t, f.httpSrv, f.key, "disable_2fa", map[string]any{"password": pw}); res.IsError {
		t.Fatalf("the password path is broken — no code + correct password was refused: %s",
			toolResultText(t, res))
	}
	if enabled, _ := f.totpEnabled(t); enabled {
		t.Error("2FA was not disabled by the password-only control call")
	}
}

// TestDisable2FAWithoutEitherProofIsRefused is the other direction: nothing that
// fails to be exactly one valid proof may turn 2FA off. Without it, an
// implementation that "made the docs true" by disabling unconditionally would
// pass the positive test above.
//
// The fixture user's password_hash is the harness's unusable 'it' value, so every
// argument set below is one a real caller could send with nothing but a leaked
// API key.
func TestDisable2FAWithoutEitherProofIsRefused(t *testing.T) {
	requireTestDB(t)
	f := newDisable2faFixture(t, "mcpnoproof")

	for _, tc := range []struct {
		name      string
		args      map[string]any
		bySchema  bool   // refused by the contract (before the handler runs)
		schemaMsg string // marker that proves the refusal came from the schema
	}{
		{name: "no fields at all", args: map[string]any{}, bySchema: true, schemaMsg: "oneOf"},
		{name: "both proofs at once", args: map[string]any{"totp_code": "123456", "password": "x"}, bySchema: true, schemaMsg: "oneOf"},
		{name: "unknown field only", args: map[string]any{"nope": "x"}, bySchema: true, schemaMsg: `validating "arguments"`},
		{name: "wrong password only", args: map[string]any{"password": "not the password"}},
		{name: "wrong 2FA code only", args: map[string]any{"totp_code": "000000"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := mcpCall(t, f.httpSrv, f.key, "disable_2fa", tc.args)
			if !res.IsError {
				t.Fatalf("a call with %s was ACCEPTED (reply %s) — an API key alone must never "+
					"disable a second factor", tc.name, toolResultText(t, res))
			}
			if tc.bySchema {
				if msg := toolResultText(t, res); !strings.Contains(msg, tc.schemaMsg) {
					t.Errorf("a call with %s was refused by the HANDLER, not by the input contract "+
						"(want %q in the message): %s", tc.name, tc.schemaMsg, msg)
				}
			}
			if enabled, _ := f.totpEnabled(t); !enabled {
				t.Fatalf("after a refused call (%s) 2FA is OFF — the write ran despite the refusal", tc.name)
			}
		})
	}
}

// TestDisable2FAIsRefusedForAReadonlyKey is the scope half (issue #645): every
// positive test above uses a readwrite key, so without this a change that dropped
// the write gate would still look green.
func TestDisable2FAIsRefusedForAReadonlyKey(t *testing.T) {
	requireTestDB(t)
	ctx := context.Background()
	pool := testdb.Open(t)
	accountID, userID := scopeTestAccount(t, pool, "mcproreadonly")
	users := dbstore.NewUserStore(pool)
	secret, _, err := auth.GenerateTOTPSecret("mcp-ro@example.test")
	if err != nil {
		t.Fatalf("generate TOTP secret: %v", err)
	}
	if err := users.EnableTOTP(ctx, userID, secret); err != nil {
		t.Fatalf("enable TOTP: %v", err)
	}
	keys := dbstore.NewAPIKeyStore(pool)
	roKey := scopeTestKey(t, ctx, keys, accountID, "mcp-ro-key", []string{dbstore.ScopeReadonly})
	// A real session store, so the refusal asserted below comes from the WRITE
	// SCOPE gate and not from #713's nil-store guard — which runs after it.
	// With Sessions left nil this case would still "pass" while proving nothing
	// about the scope gate, because EVERY call would be refused by the other one.
	srv := New(&Deps{
		Users: users, Keys: keys, Accounts: dbstore.NewAccountStore(pool),
		Forms: formsFor(pool), Sessions: valkeySessionsForTest(t),
	}, nil, nil, nil)
	httpSrv := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(httpSrv.Close)

	if res := mcpCall(t, httpSrv.URL, roKey, "disable_2fa",
		map[string]any{"totp_code": currentTOTPCode(t, secret)}); !res.IsError {
		t.Fatalf("a readonly key disabled 2FA with a valid code (reply %s) — the write scope "+
			"gate must run before the proof", toolResultText(t, res))
	}
	var enabled bool
	if err := pool.QueryRow(ctx, `SELECT totp_enabled FROM users WHERE id = $1`, userID).Scan(&enabled); err != nil {
		t.Fatalf("read users row: %v", err)
	}
	if !enabled {
		t.Error("2FA is off after a readonly-key call — the gate is too late")
	}
}
