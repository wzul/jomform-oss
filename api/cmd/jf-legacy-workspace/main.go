// Command jf-legacy-workspace applies the legacy onboarding rule to ONE existing
// user at a time.
//
// WHY A COMMAND AND NOT A MIGRATION
//
//	Wan's rule (2026-09-20) provisions a workspace for an existing user who
//	already owns forms but has none. That is a per-user data decision, not a
//	schema change: the affected accounts were enumerated and measured one by
//	one, some accounts are excluded on purpose (the template gallery), and the
//	others must be ASKED rather than provisioned. A migration would run the same
//	blanket UPDATE on every environment and could not carry any of those
//	distinctions — which is exactly the no-WHERE UPDATE the card forbids.
//
//	This command takes ONE --user and refuses to run without it, so a blanket
//	sweep is not expressible. It calls the same
//	dbstore.WorkspaceStore.ProvisionLegacyWorkspaceForOwner the login path calls,
//	so the prod run executes the code the tests exercise rather than a
//	hand-written SQL approximation of it.
//
// USAGE
//
//	jf-legacy-workspace plan  --user <id|email>     # read-only: what would happen
//	jf-legacy-workspace apply --user <id|email>     # provision, print read-back
//
// DATABASE_URL must be set. `plan` writes nothing.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) < 1 {
		return errors.New("usage: jf-legacy-workspace <plan|apply> --user <id|email>")
	}
	mode := args[0]
	if mode != "plan" && mode != "apply" {
		return fmt.Errorf("unknown mode %q (want plan|apply)", mode)
	}
	user := flagValue(args[1:], "--user")
	if user == "" {
		return errors.New("--user is required: this command provisions ONE user and has no all-users mode")
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	userID, email, err := resolveUser(ctx, pool, user)
	if err != nil {
		return err
	}
	fmt.Printf("user=%s email=%s\n", userID, email)

	sp := dbstore.NewWorkspaceStore(pool)
	accountID, err := sp.LegacyUnscopedAccount(ctx, userID)
	if err != nil {
		return err
	}
	if accountID == "" {
		fmt.Println("applicable=no (no workspace-less form-owning account, or a workspace already exists)")
		return reportState(ctx, pool, userID, accountID)
	}
	fmt.Printf("applicable=yes account=%s\n", accountID)
	if mode == "plan" {
		wsSlug := dbstore.SlugFromEmailLocalPart(email, userID)
		fmt.Printf("would_create: name=%q slug=%q\n", workspaceNameFromEmail(email, wsSlug), wsSlug)
		return reportState(ctx, pool, userID, accountID)
	}

	ws, err := sp.ProvisionLegacyWorkspaceForOwner(ctx, userID)
	if err != nil {
		return err
	}
	if ws == nil {
		fmt.Println("provisioned=no (nothing to do — idempotent no-op)")
	} else {
		fmt.Printf("provisioned=yes workspace=%s name=%q slug=%q\n", ws.ID, ws.Name, ws.Slug)
	}
	return reportState(ctx, pool, userID, accountID)
}

// reportState prints the read-back an operator needs to prove what happened:
// exactly one workspace and one account for the user, and the count of rows
// still carrying a NULL workspace_id.
func reportState(ctx context.Context, pool *pgxpool.Pool, userID, accountID string) error {
	var workspaces, accounts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, userID).Scan(&workspaces); err != nil {
		return err
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE owner_user_id=$1`, userID).Scan(&accounts); err != nil {
		return err
	}
	fmt.Printf("readback: workspaces=%d accounts=%d\n", workspaces, accounts)
	if accountID == "" {
		return nil
	}
	for _, tbl := range []string{"forms", "products", "sales", "api_keys"} {
		var unscoped, total int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FILTER (WHERE workspace_id IS NULL), count(*) FROM `+tbl+` WHERE account_id=$1`,
			accountID).Scan(&unscoped, &total); err != nil {
			return err
		}
		fmt.Printf("readback: %s total=%d workspace_id_null=%d\n", tbl, total, unscoped)
	}
	return nil
}

// resolveUser accepts a uuid or an email, so an operator can name the user the
// way the card enumerates them (by email) without a lookup step.
func resolveUser(ctx context.Context, pool *pgxpool.Pool, user string) (id, email string, err error) {
	if strings.Contains(user, "@") {
		err = pool.QueryRow(ctx, `SELECT id, email FROM users WHERE email=$1`, user).Scan(&id, &email)
		return id, email, err
	}
	err = pool.QueryRow(ctx, `SELECT id, COALESCE(email,'') FROM users WHERE id=$1`, user).Scan(&id, &email)
	return id, email, err
}

func workspaceNameFromEmail(email, fallback string) string {
	name := email
	if at := strings.Index(name, "@"); at > 0 {
		name = name[:at]
	}
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"=")
		}
	}
	return ""
}
