// The chat edge carries the caller's DASHBOARD language to the model (card
// t_ec87d6c7).
//
// WHY THIS TEST IS AT THE EDGE AND NOT IN internal/chat. The prompt half is
// pinned there (prompt_card_language_test.go): given a language, the system prompt
// names it. What that package cannot prove is that the language ever ARRIVES — the
// realistic half-fix is a correct prompt builder whose language parameter is
// always empty because nobody read the column. That wiring lives here, in
// handleChat, and it is the difference between an English card and the Malay one
// the card reported.
//
// The value is read from users.lang, the same column the dashboard resolves its
// own locale from, and NOT from the request body: the card's meaning must not
// depend on a field any client can set.
//
// DB-backed (a real users row is the source of the value), skipped without the
// standard test env like the rest of this file's package.
package httpapi

import (
	"context"
	"net/http"
	"testing"
)

// TestChatCarriesTheProfileLanguageToTheModel is the wiring half of t_ec87d6c7.
func TestChatCarriesTheProfileLanguageToTheModel(t *testing.T) {
	runner := &recordingRunner{ready: true}
	e := newChatEnv(t, runner)
	userID, _, token := e.newUser(t)

	// The merchant picked Malay in the dashboard language picker.
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE users SET lang='ms' WHERE id=$1`, userID); err != nil {
		t.Fatalf("set users.lang: %v", err)
	}

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"berapa banyak borang saya?"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/rest/chat = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	runner.mu.Lock()
	got := runner.gotLang
	runner.mu.Unlock()
	if got != "ms" {
		t.Fatalf("the chat runner was given dashboard language %q, want \"ms\" — the model is then "+
			"never told that the confirmation card is in Malay, and it writes the card's one "+
			"model-authored sentence in the conversation's language instead (the reported "+
			"defect, on a Malay dashboard)", got)
	}
}

// TestChatWithoutAProfileLanguageLeavesTheCardEnglish: the column is empty until
// a merchant picks a language, and an empty value must stay "not set" all the way
// through the edge — the prompt then resolves it to English, the dashboard's own
// default. It must not become an error, and it must not become the empty string in
// the prompt (which would name no language at all).
func TestChatWithoutAProfileLanguageLeavesTheCardEnglish(t *testing.T) {
	runner := &recordingRunner{ready: true}
	e := newChatEnv(t, runner)
	_, _, token := e.newUser(t) // users.lang defaults to unset

	rec := e.do(t, token, http.MethodPost, "/v1/rest/chat", `{"message":"how many forms do I have?"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/rest/chat = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	runner.mu.Lock()
	got := runner.gotLang
	runner.mu.Unlock()
	if got != "" {
		t.Fatalf("the chat runner was given dashboard language %q for a user who never picked "+
			"one, want \"\" (not set → English in the prompt)", got)
	}
}
