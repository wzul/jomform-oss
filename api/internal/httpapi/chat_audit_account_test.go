package httpapi

// THE ACCOUNT A CHAT LANE AUDIT ROW IS FILED ON (card t_fdb74390).
//
// THE DEFECT. recordChatEvent and recordChatConfirmEvent attributed their rows to
// Accounts.ByOwner(r.Context(), uid)[0] — the session user's FIRST owned account,
// ordering key accounts.created_at — while every other part of the lane resolved
// ?workspace= correctly (the identity the tools run under, the account a proposal
// is stored on, the account a confirmed write lands in). So a conversation held
// under ?workspace=<second> filed its chat.message / chat.proposal_confirmed rows
// inside the FIRST owned workspace's account. events.account_id is what the
// account-scoped Activity history is read through, so the second workspace's own
// history never showed the conversation, and the first workspace's showed one
// that never happened there. A single-account user is unaffected either way,
// which is why nothing caught it. Reproduced from the card's probe: a request
// scoped to tl-b-ad67a3dd (account e6162618-…) landed its chat.message on
// 08980b46-…, the FIRST workspace's account.
//
// WHAT IS ASSERTED, AND WHY BOTH THINGS. The row's account_id is what was wrong,
// and GET /v1/rest/events?workspace= is what a merchant actually reads (it
// filters by the workspace's account set). Asserting the row alone would let a
// broken read scope through; asserting the read alone would let the row move
// somewhere the read scope only appears to cover. So: the rows are read straight
// out of Postgres, and the same history is then read through the real endpoint.
//
// BOTH DIRECTIONS, AND THE THIRD ROUTE. Each workspace is selected in turn and
// the OTHER one must end up with nothing, because "always the second" is the same
// defect as "always the first". All three audited call sites are driven on the
// SAME selected scope — a message, a confirmation and a cancellation — so a fix
// applied to one call site and forgotten at another cannot pass.
//
// Integration against a real Postgres + Valkey, skipped without the test env.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// newChatAuditEnv is this file's fixture: the two-workspace session fixture the
// chat scope lane already owns (newScopeEnv, with its scripted runner and the
// real workspace store), plus the Events store the audit rows are written to. The
// store is the one thing that fixture does not wire, and the audit row is what
// this file asserts — set on the same *RESTHandlers the mux's routes close over,
// so enabling it here is what production already does.
func newChatAuditEnv(t *testing.T) *scopeEnv {
	t.Helper()
	e := newScopeEnv(t)
	e.h.Events = dbstore.NewEventStore(e.pool)
	return e
}

// chatAuditRows reads every chat audit row a user wrote, keyed by event name,
// straight out of Postgres — never through the handler that wrote them.
func (e *scopeEnv) chatAuditRows(t *testing.T, userID string) map[string][]string {
	t.Helper()
	rows, err := e.pool.Query(context.Background(),
		`SELECT event_name, account_id::text FROM events
		  WHERE user_id = $1 AND event_name IN ('chat.message','chat.proposal_confirmed')
		  ORDER BY event_name, id`, userID)
	if err != nil {
		t.Fatalf("read chat audit rows: %v", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var name, account string
		if err := rows.Scan(&name, &account); err != nil {
			t.Fatalf("scan chat audit row: %v", err)
		}
		out[name] = append(out[name], account)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate chat audit rows: %v", err)
	}
	return out
}

// chatHistoryFor is the merchant's own view of this lane: the event names
// GET /v1/rest/events?workspace=<slug> answers with, which is the account-scoped
// Activity history the row's account_id decides the visibility of.
func (e *scopeEnv) chatHistoryFor(t *testing.T, token, slug string) []string {
	t.Helper()
	rec := e.do(t, token, http.MethodGet, "/v1/rest/events?workspace="+slug+"&event_name=chat.*", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/events?workspace=%s = %d, want 200 (body %s)",
			slug, rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			EventName string `json:"event_name"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("events response is not the documented envelope: %v (body %s)", err, rec.Body.String())
	}
	if int64(len(body.Items)) != body.Total {
		t.Fatalf("events envelope is inconsistent: %d items for total %d", len(body.Items), body.Total)
	}
	names := make([]string, 0, len(body.Items))
	for _, it := range body.Items {
		names = append(names, it.EventName)
	}
	return names
}

// TestChatAuditRowsFollowTheResolvedWorkspace is the card's criterion: with two
// workspaces, a chat held under ?workspace=<one of them> files its
// chat.message and chat.proposal_confirmed rows on THAT workspace's account, and
// the other workspace's account gets no row at all.
func TestChatAuditRowsFollowTheResolvedWorkspace(t *testing.T) {
	for _, selected := range []int{1, 0} {
		selected := selected
		t.Run(fmt.Sprintf("selected_workspace_%d", selected), func(t *testing.T) {
			other := 1 - selected
			e := newChatAuditEnv(t)
			userID, token, slugs := e.userWithWorkspaces(t, 2)
			first := e.accountForWorkspace(t, slugs[0])
			second := e.accountForWorkspace(t, slugs[1])
			if first == second {
				t.Fatal("the two workspaces resolved to the same account — the fixture cannot distinguish them")
			}

			// THE PREMISE, checked rather than assumed: the attribution this card
			// fixes picked Accounts.ByOwner(...)[0], so the fixture must have the
			// FIRST workspace's account as that row — otherwise the "the other
			// workspace got nothing" half below would pass on the old code too.
			accs, err := dbstore.NewAccountStore(e.pool).ByOwner(context.Background(), userID)
			if err != nil || len(accs) != 2 {
				t.Fatalf("list owned accounts: n=%d err=%v", len(accs), err)
			}
			if accs[0].ID != first {
				t.Fatalf("Accounts.ByOwner(...)[0] is %s, but the fixture's FIRST workspace is %s — "+
					"the premise (that the old attribution picked the unselected workspace) does not hold",
					accs[0].ID, first)
			}

			want := []string{first, second}[selected]
			unwanted := []string{first, second}[other]

			// All three audited call sites, on the SAME selected scope: an answer,
			// a confirmation and a cancellation. Nothing in this test ever names
			// the other workspace.
			rec := e.do(t, token, http.MethodPost, "/v1/rest/chat?workspace="+slugs[selected],
				`{"message":"berapa produk saya?","history":[]}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("scoped chat POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			rec = e.do(t, token, http.MethodPost, "/v1/rest/chat/confirm?workspace="+slugs[selected],
				`{"token":"tok-audit","args_hash":"hash-audit"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("scoped chat confirm = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			rec = e.do(t, token, http.MethodPost, "/v1/rest/chat/cancel?workspace="+slugs[selected],
				`{"token":"tok-audit-cancel"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("scoped chat cancel = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}

			rows := e.chatAuditRows(t, userID)
			for name, wantCount := range map[string]int{"chat.message": 1, "chat.proposal_confirmed": 2} {
				got := rows[name]
				if len(got) != wantCount {
					t.Fatalf("%s rows for this user = %v, want %d — the three chat routes must each "+
						"audit, on the workspace the request resolved (%s)",
						name, got, wantCount, slugs[selected])
				}
				for _, account := range got {
					if account != want {
						t.Fatalf("%s landed on account %s, want the RESOLVED workspace's %s "+
							"(the first owned workspace is %s): after a chat under ?workspace=%s, the "+
							"audit row is what makes the conversation visible in that workspace's "+
							"Activity history — and only there",
							name, account, want, first, slugs[selected])
					}
				}
			}
			// The unselected workspace gets NOTHING. Counted over every account
			// rather than checked per name, so a lane that wrote the same row
			// twice — once per workspace — fails here too.
			total := 0
			for name, ids := range rows {
				for _, account := range ids {
					total++
					if account == unwanted {
						t.Errorf("%s landed on account %s — that is the workspace NOT selected by this "+
							"request (?workspace=%s)", name, unwanted, slugs[selected])
					}
				}
			}
			if total != 3 {
				t.Fatalf("chat audit rows = %d, want 3 (one message, one confirmation, one "+
					"cancellation)", total)
			}

			// The merchant's own view. The selected workspace's Activity history
			// shows all three; the other workspace's shows none of them.
			if got := e.chatHistoryFor(t, token, slugs[selected]); len(got) != 3 {
				t.Errorf("GET /v1/rest/events?workspace=%s answered %v, want the 3 chat rows just "+
					"written — the audit row is filed where the history is read",
					slugs[selected], got)
			}
			if got := e.chatHistoryFor(t, token, slugs[other]); len(got) != 0 {
				t.Errorf("GET /v1/rest/events?workspace=%s answered %v, want nothing — a conversation "+
					"held in %s must not appear in another workspace's history",
					slugs[other], got, slugs[selected])
			}
		})
	}
}
