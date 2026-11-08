// Chat history: the store's scoping, naming and retention rules.
//
// THE PROPERTIES THIS FILE OWNS, because a rendering test cannot see any of
// them:
//
//  1. A CONVERSATION IS THE PERSON'S AND THE WORKSPACE'S. Another user's id
//     reads as "not found" — not as their conversation. The same person's OTHER
//     workspace reads the same way, so switching workspace cannot leak a
//     conversation across.
//  2. THE FIRST USER MESSAGE NAMES THE CONVERSATION, and a rename or a later
//     message does not overwrite it.
//  3. THE STORED COPY IS CAPPED at the same window the assistant is given, so a
//     long conversation cannot grow without bound.
//  4. THE DOWN MIGRATION RUNS, and the up runs again after it — a rollback that
//     cannot be run is a rollback nobody dares use.
//
// The 000086 up/down pair is exercised INSIDE this package's scratch schema
// rather than in a throwaway database: one test database, one schema per test
// process (see internal/testdb), no CREATE DATABASE.
//
// Integration against a real Postgres, skipped without the standard test env.
package dbstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// chatSessionFixture makes a user, an account and a workspace OWNING that
// account — the shape every stored conversation is scoped by.
func chatSessionFixture(t *testing.T, ctx context.Context, p *pgxpool.Pool) (userID, workspaceID, accountID string) {
	t.Helper()
	userID = testOwnerUser(t, ctx, p)
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	ws, err := NewWorkspaceStore(p).CreateForAccount(ctx, userID,
		"chat ws "+randSuffix(), "chat-ws-"+randSuffix(), accountID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return userID, ws.ID, accountID
}

// TestChatSessionsAreScopedToThePersonAndTheWorkspace is property 1: the
// cross-user and cross-workspace refusals, at every read the API exposes.
func TestChatSessionsAreScopedToThePersonAndTheWorkspace(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewChatSessionStore(p)

	alice, aliceWS, _ := chatSessionFixture(t, ctx, p)
	bob, _, _ := chatSessionFixture(t, ctx, p)

	s, err := store.Create(ctx, alice, aliceWS, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := store.Append(ctx, alice, aliceWS, s.ID, "user", "berapa banyak borang saya?", nil, 0); err != nil {
		t.Fatalf("append user turn: %v", err)
	}

	// The owner reads it.
	got, err := store.ByID(ctx, alice, aliceWS, s.ID)
	if err != nil {
		t.Fatalf("owner ByID: %v", err)
	}
	if got.Title == "" {
		t.Fatal("the first user message did not name the conversation — the sidebar would show a blank row")
	}
	if _, err := store.Messages(ctx, alice, aliceWS, s.ID, 0); err != nil {
		t.Fatalf("owner Messages: %v", err)
	}

	// Another person does not.
	if _, err := store.ByID(ctx, bob, aliceWS, s.ID); err != ErrChatSessionNotFound {
		t.Fatalf("ByID as another user = %v, want ErrChatSessionNotFound — a conversation must not be "+
			"readable by anyone but its owner", err)
	}
	if _, err := store.Messages(ctx, bob, aliceWS, s.ID, 0); err != ErrChatSessionNotFound {
		t.Fatalf("Messages as another user = %v, want ErrChatSessionNotFound", err)
	}
	if err := store.Rename(ctx, bob, aliceWS, s.ID, "mine now"); err != ErrChatSessionNotFound {
		t.Fatalf("Rename as another user = %v, want ErrChatSessionNotFound", err)
	}
	if err := store.Delete(ctx, bob, aliceWS, s.ID); err != ErrChatSessionNotFound {
		t.Fatalf("Delete as another user = %v, want ErrChatSessionNotFound", err)
	}
	if _, err := store.Append(ctx, bob, aliceWS, s.ID, "user", "sneaking in", nil, 0); err != ErrChatSessionNotFound {
		t.Fatalf("Append as another user = %v, want ErrChatSessionNotFound", err)
	}

	// The same person from a DIFFERENT workspace does not read it either:
	// switching workspace is a different sidebar, not a wider one.
	_, aliceOtherWS, _ := chatSessionFixture(t, ctx, p)
	if _, err := store.ByID(ctx, alice, aliceOtherWS, s.ID); err != ErrChatSessionNotFound {
		t.Fatalf("ByID from another workspace = %v, want ErrChatSessionNotFound — a conversation must not "+
			"appear in a workspace it was not held in", err)
	}
	list, err := store.List(ctx, alice, aliceOtherWS, true)
	if err != nil {
		t.Fatalf("List (other workspace): %v", err)
	}
	for _, row := range list {
		if row.ID == s.ID {
			t.Fatal("the conversation is listed in the person's OTHER workspace")
		}
	}

	// And the owner's own list has it, once.
	list, err = store.List(ctx, alice, aliceWS, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := 0
	for _, row := range list {
		if row.ID == s.ID {
			found++
			if row.MessageCount != 1 {
				t.Fatalf("message_count = %d, want 1", row.MessageCount)
			}
		}
	}
	if found != 1 {
		t.Fatalf("the conversation appears %d times in its own workspace's list, want 1", found)
	}
}

// TestChatSessionWithoutWorkspaceIsItsOwnScope covers the nullable-workspace
// state: a signup creates an account with no workspace, and that person must
// still be able to keep a conversation — in a scope that is NOT the same as any
// workspace's.
func TestChatSessionWithoutWorkspaceIsItsOwnScope(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewChatSessionStore(p)

	uid := testOwnerUser(t, ctx, p)
	s, err := store.Create(ctx, uid, "", "")
	if err != nil {
		t.Fatalf("create session with no workspace: %v", err)
	}
	if _, err := store.Append(ctx, uid, "", s.ID, "user", "hello", nil, 0); err != nil {
		t.Fatalf("append: %v", err)
	}
	// It is readable in the no-workspace scope.
	if _, err := store.ByID(ctx, uid, "", s.ID); err != nil {
		t.Fatalf("ByID in the no-workspace scope: %v", err)
	}
	// It is NOT readable once the person has a workspace: that is a different
	// list, not a superset.
	_, wsID, _ := chatSessionFixture(t, ctx, p)
	list, err := store.List(ctx, uid, wsID, true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, row := range list {
		if row.ID == s.ID {
			t.Fatal("a no-workspace conversation leaked into a workspace's list — the two scopes must not mix")
		}
	}
	// And a workspace conversation does not leak back into the unscoped scope.
	if _, err := store.ByID(ctx, uid, "", s.ID); err != nil {
		t.Fatalf("the unscoped conversation stopped resolving: %v", err)
	}
}

// TestChatSessionNamesItselfOnceAndKeepsTheFirstLabel is property 2.
func TestChatSessionNamesItselfOnceAndKeepsTheFirstLabel(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewChatSessionStore(p)
	uid, ws, _ := chatSessionFixture(t, ctx, p)

	s, err := store.Create(ctx, uid, ws, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// An assistant turn first must NOT name it: the user has not said anything.
	if _, err := store.Append(ctx, uid, ws, s.ID, "assistant", "Hello! How can I help?", nil, 0); err != nil {
		t.Fatalf("append assistant: %v", err)
	}
	got, err := store.ByID(ctx, uid, ws, s.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Title != "" {
		t.Fatalf("title = %q after only an assistant turn, want \"\" — the label is the USER's question", got.Title)
	}

	first := "berapa banyak borang saya bulan ini?"
	if _, err := store.Append(ctx, uid, ws, s.ID, "user", first, nil, 0); err != nil {
		t.Fatalf("append user: %v", err)
	}
	got, _ = store.ByID(ctx, uid, ws, s.ID)
	if got.Title != first {
		t.Fatalf("title = %q, want %q", got.Title, first)
	}

	// A second question does not rename it.
	if _, err := store.Append(ctx, uid, ws, s.ID, "user", "and how about last month?", nil, 0); err != nil {
		t.Fatalf("append second user turn: %v", err)
	}
	got, _ = store.ByID(ctx, uid, ws, s.ID)
	if got.Title != first {
		t.Fatalf("title changed to %q on the second message, want the first %q", got.Title, first)
	}

	// A rename is the user's, and sticks.
	if err := store.Rename(ctx, uid, ws, s.ID, "  Borang   bulan lepas  "); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, _ = store.ByID(ctx, uid, ws, s.ID)
	if got.Title != "Borang bulan lepas" {
		t.Fatalf("title after rename = %q, want the whitespace-collapsed label", got.Title)
	}

	// A very long first message is cut to a label, rune-safely.
	long := strings.Repeat("borang ", 40)
	s2, err := store.Create(ctx, uid, ws, "")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if _, err := store.Append(ctx, uid, ws, s2.ID, "user", long, nil, 0); err != nil {
		t.Fatalf("append long: %v", err)
	}
	got, _ = store.ByID(ctx, uid, ws, s2.ID)
	if n := len([]rune(got.Title)); n != MaxChatSessionTitleRunes {
		t.Fatalf("derived title is %d runes, want %d", n, MaxChatSessionTitleRunes)
	}
	if !strings.HasSuffix(got.Title, "…") {
		t.Fatalf("derived title %q was cut without an ellipsis", got.Title)
	}
}

// TestChatSessionKeepsOnlyTheModelWindow is property 3: the retention cap.
func TestChatSessionKeepsOnlyTheModelWindow(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewChatSessionStore(p)
	uid, ws, _ := chatSessionFixture(t, ctx, p)

	s, err := store.Create(ctx, uid, ws, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	const keep = 4
	for i := 0; i < 12; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if _, err := store.Append(ctx, uid, ws, s.ID, role, fmt.Sprintf("turn-%02d", i), nil, keep); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	msgs, err := store.Messages(ctx, uid, ws, s.ID, 0)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != keep {
		t.Fatalf("stored %d messages, want the cap of %d — an unbounded conversation is a cost and a "+
			"privacy problem", len(msgs), keep)
	}
	// The survivors are the NEWEST ones, in order.
	if msgs[len(msgs)-1].Content != "turn-11" {
		t.Fatalf("last stored message = %q, want the newest turn", msgs[len(msgs)-1].Content)
	}
	if msgs[0].Content != "turn-08" {
		t.Fatalf("first stored message = %q, want turn-08 (the 4 newest are 08..11)", msgs[0].Content)
	}
}

// TestChatSessionArchiveAndDelete covers the two ways a conversation leaves the
// sidebar: archiving (reversible, hidden from the default list) and deletion
// (gone, messages and all).
func TestChatSessionArchiveAndDelete(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewChatSessionStore(p)
	uid, ws, _ := chatSessionFixture(t, ctx, p)

	s, err := store.Create(ctx, uid, ws, "keep me")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.SetArchived(ctx, uid, ws, s.ID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	live, err := store.List(ctx, uid, ws, false)
	if err != nil {
		t.Fatalf("List live: %v", err)
	}
	for _, row := range live {
		if row.ID == s.ID {
			t.Fatal("an archived conversation is still in the live list")
		}
	}
	all, err := store.List(ctx, uid, ws, true)
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	var sawArchived bool
	for _, row := range all {
		if row.ID == s.ID {
			sawArchived = row.ArchivedAt != nil
		}
	}
	if !sawArchived {
		t.Fatal("the archived conversation is not reported as archived")
	}
	if err := store.SetArchived(ctx, uid, ws, s.ID, false); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	live, _ = store.List(ctx, uid, ws, false)
	found := false
	for _, row := range live {
		if row.ID == s.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("an unarchived conversation did not come back to the live list")
	}

	if _, err := store.Append(ctx, uid, ws, s.ID, "user", "question", nil, 0); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := store.Delete(ctx, uid, ws, s.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.ByID(ctx, uid, ws, s.ID); err != ErrChatSessionNotFound {
		t.Fatalf("ByID after delete = %v, want ErrChatSessionNotFound", err)
	}
	var orphans int
	if err := p.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_messages WHERE session_id = $1`, s.ID).Scan(&orphans); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("%d messages survived their conversation's delete, want 0 (the FK must cascade)", orphans)
	}
}

// TestChatSessionTitleTruncationIsRuneSafe pins the label cut on multi-byte text.
// The label is often Malay or Chinese; cutting mid-rune would store invalid UTF-8
// and Postgres would reject the user's first message.
func TestChatSessionTitleTruncationIsRuneSafe(t *testing.T) {
	in := strings.Repeat("语", MaxChatSessionTitleRunes+20)
	out := truncateTitle(in)
	if n := len([]rune(out)); n != MaxChatSessionTitleRunes {
		t.Fatalf("truncated to %d runes, want %d", n, MaxChatSessionTitleRunes)
	}
	if !strings.HasSuffix(out, "…") {
		t.Fatal("a cut title must say it was cut")
	}
	// A short title is untouched, including one with a newline (a pasted
	// multi-line draft becomes a single-line label).
	if got := truncateTitle("  hello\n\nworld  "); got != "hello world" {
		t.Fatalf("truncateTitle(%q) = %q, want %q", "  hello\n\nworld  ", got, "hello world")
	}
}

// TestChatSessionsMigrationDownAndUp is property 4: the 000086 pair runs.
//
// It runs inside this package's scratch schema (already migrated by testdb.Open),
// so the UP is proven twice — by the harness at startup and again here after a
// down — and the DOWN is proven runnable and repeatable. No throwaway database is
// created: one test database, one schema per process.
//
// THE DOWN RUNS WITH search_path PINNED TO THIS SCHEMA. The pool's search_path is
// `"<scratch>", public`, so a second `DROP TABLE IF EXISTS chat_messages` — the
// repeatability check — would not find the table in the scratch schema any more
// and would fall through to PUBLIC, dropping the shared test database's table.
// Pinning the path for the transaction is what makes running the rollback twice a
// no-op instead of an accident.
func TestChatSessionsMigrationDownAndUp(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	dir := filepath.Join("..", "db", "migrations")
	upPath := filepath.Join(dir, "000086_chat_sessions.up.sql")
	downPath := filepath.Join(dir, "000086_chat_sessions.down.sql")
	up, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatalf("read %s: %v", upPath, err)
	}
	down, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatalf("read %s: %v — every up migration must ship a down (TestEveryMigrationHasDownFile)", downPath, err)
	}

	var schema string
	if err := p.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatalf("resolve the scratch schema: %v", err)
	}
	if schema == "" || schema == "public" {
		t.Fatalf("refusing to run a rollback against schema %q — this must be a scratch schema", schema)
	}
	only := pgx.Identifier{schema}.Sanitize()

	// DOWN: the tables go, and a second run is a no-op (IF EXISTS), because a
	// rollback nobody dares run twice is a rollback nobody dares run.
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL search_path TO `+only); err != nil {
		t.Fatalf("pin search_path: %v", err)
	}
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("000086 down did not apply: %v", err)
	}
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("000086 down is not repeatable: %v", err)
	}
	var present int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.tables
		  WHERE table_schema = current_schema() AND table_name IN ('chat_sessions','chat_messages')`).
		Scan(&present); err != nil {
		t.Fatalf("inspect tables: %v", err)
	}
	if present != 0 {
		t.Fatalf("%d chat-history tables survived the down migration, want 0", present)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit down: %v", err)
	}

	// UP: the tables come back, and the store works on them again. This also
	// leaves the shared schema migrated for every test after this one.
	if _, err := p.Exec(ctx, string(up)); err != nil {
		t.Fatalf("000086 up did not re-apply after the down: %v", err)
	}
	uid, ws, _ := chatSessionFixture(t, ctx, p)
	store := NewChatSessionStore(p)
	s, err := store.Create(ctx, uid, ws, "after re-up")
	if err != nil {
		t.Fatalf("create after re-up: %v", err)
	}
	if _, err := store.Append(ctx, uid, ws, s.ID, "user", "hi", nil, 0); err != nil {
		t.Fatalf("append after re-up: %v", err)
	}
}
