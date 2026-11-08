// Chat history: the conversations a person has with the in-app assistant, and
// the messages inside them.
//
// WHY THIS IS SERVER-SIDE. The transcript used to live in localStorage and was
// deleted when the session ended, so logging out destroyed every conversation
// the person had had. A conversation is the user's own work — the questions they
// asked and the answers they read — and it has to survive the browser, a second
// device and a new login.
//
// SCOPING IS THE WHOLE SECURITY MODEL HERE, and it is enforced in THIS file
// rather than trusted to callers: every method takes the caller's (user, scope)
// pair and filters on it, so a session id guessed from anywhere else reads as
// ErrChatSessionNotFound — the same answer a session that never existed gets, and
// the same no-existence-oracle rule the proposal and feedback stores follow.
//
// WHAT IS NOT STORED. A proposal's confirmation token. A card is a single-use
// capability with a five-minute life (000067_chat_write_proposals); a transcript
// keeps the words, the tool trail and nothing that could still be spent.
package dbstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrChatSessionNotFound means no session with this id belongs to this caller in
// this scope. It covers "never existed", "belongs to somebody else" and "belongs
// to another workspace of the same person" — deliberately indistinguishable.
var ErrChatSessionNotFound = errors.New("chat session not found")

// MaxChatSessionTitleRunes bounds the sidebar label. A derived title is cut from
// the first user message, and a rename is truncated rather than refused: a user
// who pastes a paragraph into the name field wants a usable label, not a
// validation error over a control that changes nothing else.
const MaxChatSessionTitleRunes = 80

// ChatSession is one stored conversation.
type ChatSession struct {
	ID     string
	UserID string
	// WorkspaceID is "" when the person has no workspace yet (accounts.workspace_id
	// is nullable, and a signup starts without one). That state is its own scope,
	// not a wildcard: see the IS NOT DISTINCT FROM predicate in this file.
	WorkspaceID string
	Title       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ArchivedAt  *time.Time
	// MessageCount is how many turns the conversation holds, for the sidebar.
	MessageCount int
}

// ChatMessage is one turn. Meta carries what the assistant did (the tool trail),
// never a proposal token and never a sentence.
type ChatMessage struct {
	ID        string
	SessionID string
	Role      string
	Content   string
	CreatedAt time.Time
	Meta      map[string]any
}

// ChatSessionStore wraps pgx for chat_sessions + chat_messages.
type ChatSessionStore struct{ pool *pgxpool.Pool }

// NewChatSessionStore builds a ChatSessionStore.
func NewChatSessionStore(pool *pgxpool.Pool) *ChatSessionStore {
	return &ChatSessionStore{pool: pool}
}

// chatSessionSelect is the column list every read in this file scans, kept as one
// constant so a column added by a migration has one place to be added. The scan
// order is asserted by scanChatSession.
const chatSessionSelect = `s.id, s.user_id, COALESCE(s.workspace_id::text, ''), s.title,
	s.created_at, s.updated_at, s.archived_at`

// chatSessionScope is the WHERE fragment that binds a query to the caller. $1 is
// the user id and $2 the workspace id, with "" meaning the no-workspace scope.
//
// IS NOT DISTINCT FROM, not `=`: the no-workspace scope must match rows whose
// workspace_id IS NULL, and `= NULL` matches nothing. NULLIF turns the empty
// string the API uses for "no workspace" into a real NULL so one predicate covers
// both scopes.
const chatSessionScope = `s.user_id = $1 AND s.workspace_id IS NOT DISTINCT FROM NULLIF($2, '')::uuid`

// Create stores an empty conversation and fills in its row.
//
// workspaceID may be "" (the account has no workspace yet). userID may not: a
// conversation that cannot be scoped to a person must not be stored, because the
// alternative is a row two people would share.
func (s *ChatSessionStore) Create(ctx context.Context, userID, workspaceID, title string) (*ChatSession, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, errors.New("chat session needs a user id")
	}
	out := &ChatSession{UserID: userID, WorkspaceID: workspaceID, Title: truncateTitle(title)}
	var id string
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO chat_sessions (user_id, workspace_id, title)
		 VALUES ($1, NULLIF($2, '')::uuid, $3)
		 RETURNING id`,
		userID, workspaceID, out.Title).Scan(&id); err != nil {
		return nil, err
	}
	// Read the row back through the scoped read rather than scanning a second
	// copy of the column list here: one definition of "what a session looks
	// like" is what keeps a migration's new column from being added to the
	// select and forgotten at an insert site.
	return s.ByID(ctx, userID, workspaceID, id)
}

// List returns the caller's conversations in this scope, most recently used
// first. Archived conversations are included only when asked for: the sidebar
// shows live ones, and the archive is where they go when the user tidies up.
func (s *ChatSessionStore) List(ctx context.Context, userID, workspaceID string, includeArchived bool) ([]*ChatSession, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+chatSessionSelect+`, COUNT(m.id)
		   FROM chat_sessions s
		   LEFT JOIN chat_messages m ON m.session_id = s.id
		  WHERE `+chatSessionScope+`
		    AND ($3 OR s.archived_at IS NULL)
		  GROUP BY s.id
		  ORDER BY s.updated_at DESC, s.id DESC`,
		userID, workspaceID, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*ChatSession, 0, 8)
	for rows.Next() {
		c, err := scanChatSessionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Count returns how many conversations the caller has in this scope, archived
// ones included: the cap is about how much they have stored, not how much they
// are looking at.
func (s *ChatSessionStore) Count(ctx context.Context, userID, workspaceID string) (int, error) {
	if strings.TrimSpace(userID) == "" {
		return 0, nil
	}
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_sessions s WHERE `+chatSessionScope,
		userID, workspaceID).Scan(&n)
	return n, err
}

// ByID reads one conversation in the caller's scope.
func (s *ChatSessionStore) ByID(ctx context.Context, userID, workspaceID, id string) (*ChatSession, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(id) == "" {
		return nil, ErrChatSessionNotFound
	}
	row := s.pool.QueryRow(ctx,
		`SELECT `+chatSessionSelect+`,
		        (SELECT COUNT(*) FROM chat_messages m WHERE m.session_id = s.id)
		   FROM chat_sessions s
		  WHERE `+chatSessionScope+` AND s.id = $3`,
		userID, workspaceID, id)
	return scanChatSessionRow(row)
}

// Messages reads one conversation's turns, oldest first, capped to the newest
// `limit` turns when limit > 0.
//
// It is SCOPED, not just keyed by session id. A caller that already resolved the
// session through ByID would be safe with a plain `WHERE session_id = $1`, but
// the safety then depends on every future call site remembering to resolve first.
// Carrying the scope into this query is what keeps "user A cannot read user B's
// conversation" a property of the store.
func (s *ChatSessionStore) Messages(ctx context.Context, userID, workspaceID, sessionID string, limit int) ([]*ChatMessage, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, ErrChatSessionNotFound
	}
	// The scope check is a JOIN to the session rather than a second round trip:
	// no session in this scope means no rows, which is mapped to the store's
	// not-found sentinel below.
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM chat_sessions s WHERE `+chatSessionScope+` AND s.id = $3)`,
		userID, workspaceID, sessionID).Scan(&exists); err != nil {
		return nil, chatSessionWriteErr(err)
	}
	if !exists {
		return nil, ErrChatSessionNotFound
	}

	// A cap of 0 means "everything"; a negative cap is treated the same way
	// rather than as "keep none", which is the direction a caller never intends.
	q := `SELECT id, session_id, role, content, created_at, meta
	        FROM (
	              SELECT id, session_id, role, content, created_at, meta
	                FROM chat_messages
	               WHERE session_id = $1
	               ORDER BY created_at DESC, id DESC`
	args := []any{sessionID}
	if limit > 0 {
		q += ` LIMIT $2`
		args = append(args, limit)
	}
	q += `     ) newest ORDER BY created_at, id`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*ChatMessage, 0, 16)
	for rows.Next() {
		m, err := scanChatMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Append adds one turn to a conversation, bumps updated_at, names an unnamed
// conversation from its first user message, and keeps at most `keep` turns
// (keep <= 0 keeps everything).
//
// ONE TRANSACTION, because the three writes describe one exchange: a session
// whose updated_at moved but whose message rolled back would sort to the top of
// the sidebar with nothing new in it, and a trimmed turn that survived a failed
// append would lose history for a message the user never got.
//
// The retention trim happens here rather than at read time so a long conversation
// cannot grow without bound: the transcript the assistant is given is a window
// (chat.MaxTurns), and the stored copy is kept to the same window — the caller
// passes that constant, since this package cannot import chat (chat imports this
// one).
func (s *ChatSessionStore) Append(ctx context.Context, userID, workspaceID, sessionID, role, content string, meta map[string]any, keep int) (*ChatMessage, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, ErrChatSessionNotFound
	}
	if role != "user" && role != "assistant" {
		return nil, errors.New("chat message role must be user or assistant")
	}
	rawMeta := []byte("{}")
	if len(meta) > 0 {
		b, err := json.Marshal(meta)
		if err != nil {
			return nil, err
		}
		rawMeta = b
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The scoped UPDATE is the authorisation: no row in the caller's scope means
	// the append never happens, and it also gives us the current title so the
	// "name it from its first message" write can be decided in the same
	// transaction (rather than racing a concurrent rename).
	var title string
	if err := tx.QueryRow(ctx,
		`UPDATE chat_sessions s SET updated_at = now()
		  WHERE `+chatSessionScope+` AND s.id = $3
		  RETURNING s.title`,
		userID, workspaceID, sessionID).Scan(&title); err != nil {
		if isNoSuchRow(err) {
			return nil, ErrChatSessionNotFound
		}
		return nil, err
	}

	m := &ChatMessage{SessionID: sessionID, Role: role, Content: content, Meta: meta}
	if m.Meta == nil {
		m.Meta = map[string]any{}
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO chat_messages (session_id, role, content, meta)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		sessionID, role, content, rawMeta).Scan(&m.ID, &m.CreatedAt); err != nil {
		return nil, err
	}

	if role == "user" && strings.TrimSpace(title) == "" {
		if _, err := tx.Exec(ctx,
			`UPDATE chat_sessions SET title = $2 WHERE id = $1`,
			sessionID, truncateTitle(content)); err != nil {
			return nil, err
		}
	}

	if keep > 0 {
		if _, err := tx.Exec(ctx,
			`DELETE FROM chat_messages
			  WHERE session_id = $1
			    AND id NOT IN (
			          SELECT id FROM chat_messages
			           WHERE session_id = $1
			           ORDER BY created_at DESC, id DESC
			           LIMIT $2)`, sessionID, keep); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// Rename sets a conversation's label, in the caller's scope. updated_at is NOT
// bumped: a rename does not make the conversation the most recent one, and
// sorting the sidebar by it would move a conversation the user merely labelled.
func (s *ChatSessionStore) Rename(ctx context.Context, userID, workspaceID, id, title string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE chat_sessions s SET title = $4
		  WHERE `+chatSessionScope+` AND s.id = $3`,
		userID, workspaceID, id, truncateTitle(title))
	if err != nil {
		return chatSessionWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChatSessionNotFound
	}
	return nil
}

// SetArchived moves a conversation in or out of the archive, in the caller's
// scope. Archiving is reversible; Delete is not.
func (s *ChatSessionStore) SetArchived(ctx context.Context, userID, workspaceID, id string, archived bool) error {
	var at any
	if archived {
		at = time.Now().UTC()
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE chat_sessions s SET archived_at = $4
		  WHERE `+chatSessionScope+` AND s.id = $3`,
		userID, workspaceID, id, at)
	if err != nil {
		return chatSessionWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChatSessionNotFound
	}
	return nil
}

// Delete removes a conversation and its messages, in the caller's scope.
func (s *ChatSessionStore) Delete(ctx context.Context, userID, workspaceID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM chat_sessions s WHERE `+chatSessionScope+` AND s.id = $3`,
		userID, workspaceID, id)
	if err != nil {
		return chatSessionWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChatSessionNotFound
	}
	return nil
}

// chatSessionWriteErr maps the two ways an UPDATE/DELETE on a session id can
// fail without touching a row onto the store's not-found sentinel: a malformed
// id is a text-representation error (22P02) from the uuid comparison, and it must
// answer 404 like an unknown id rather than 500 — a client-supplied id is not a
// server fault. `isNoSuchRow` already owns both that code and pgx.ErrNoRows.
func chatSessionWriteErr(err error) error {
	if isNoSuchRow(err) {
		return ErrChatSessionNotFound
	}
	return err
}

// truncateTitle turns any text into a sidebar label: one line, at most
// MaxChatSessionTitleRunes runes, with an ellipsis when it was cut. Rune-safe
// because the label is often Malay or Chinese and cutting mid-rune would store
// invalid UTF-8 (Postgres rejects it, and the error would land on a user's first
// message).
func truncateTitle(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= MaxChatSessionTitleRunes {
		return s
	}
	r := []rune(s)
	return string(r[:MaxChatSessionTitleRunes-1]) + "…"
}

// scanChatSessionRow scans chatSessionSelect plus a message count, mapping "no
// row" to ErrChatSessionNotFound so a caller cannot tell "missing" from
// "somebody else's".
func scanChatSessionRow(row interface {
	Scan(dest ...any) error
}) (*ChatSession, error) {
	var c ChatSession
	if err := row.Scan(&c.ID, &c.UserID, &c.WorkspaceID, &c.Title,
		&c.CreatedAt, &c.UpdatedAt, &c.ArchivedAt, &c.MessageCount); err != nil {
		if isNoSuchRow(err) {
			return nil, ErrChatSessionNotFound
		}
		return nil, err
	}
	return &c, nil
}

// scanChatMessage scans one chat_messages row. The JSONB round trip is decoded
// into a plain map: the transcript renders the tool trail from it, so a value
// that cannot be decoded is an error rather than a silently empty trail.
func scanChatMessage(row interface {
	Scan(dest ...any) error
}) (*ChatMessage, error) {
	var m ChatMessage
	var raw []byte
	if err := row.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CreatedAt, &raw); err != nil {
		return nil, err
	}
	m.Meta = map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m.Meta); err != nil {
			return nil, err
		}
		if m.Meta == nil {
			m.Meta = map[string]any{}
		}
	}
	return &m, nil
}
