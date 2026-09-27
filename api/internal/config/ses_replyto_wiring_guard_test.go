// Guard: SES_REPLY_TO must reach the SES sender.
//
// PR 733 added a monitored Reply-To mailbox for every transactional send, wired
// through three pieces:
//
//	env SES_REPLY_TO   -> config.SESReplyTo    (config.Load)
//	config.SESReplyTo  -> SMTPConfig.ReplyTo   (main.go — TWO senders)
//	SMTPConfig.ReplyTo -> the Reply-To header  (email/ses.go)
//
// The last hop is covered by replyto_issue_feedback_test.go, but every test
// there BUILDS the SMTPConfig by hand (email.SMTPConfig{ReplyTo: []string{…}})
// and asserts on the request SES received. That proves the header *given a
// configured sender* — and a sender that is never configured is exactly what
// ships if either wiring step above is dropped, reordered, or misses one of
// main.go's two SMTPConfig literals. The suite stays green either way, and
// replies go to noreply@jomform.com where nothing reads them: the silent-loss
// defect this feature exists to close.
//
// So this file asserts the two hops nothing covered. Both are derived from the
// source — the real config.Load, and main.go parsed with go/ast — rather than
// from a hand-kept list, so a new SMTPConfig literal is covered the day it is
// written instead of the day someone remembers to add it here.
package config

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
)

// mainGoPath is relative to this package's directory (api/internal/config), so
// it resolves to api/cmd/jomform-api/main.go. Reading the source is deliberate
// and matches api/internal/httpapi/payments_issue557_test.go: the failure mode
// is a MISSING field in a struct literal, which no type checker can catch
// because every field of SMTPConfig is optional.
const mainGoPath = "../../cmd/jomform-api/main.go"

// minSMTPSenderLiterals is the floor. main.go builds two senders — the auth
// mailer and the payments receipt sender — each as its own email.SMTPConfig
// literal. If the scan finds fewer, the guard is no longer looking at the code
// it claims to check and must fail loudly rather than pass vacuously.
const minSMTPSenderLiterals = 2

// TestLoadSESReplyTo pins the env -> config hop.
func TestLoadSESReplyTo(t *testing.T) {
	t.Run("configured", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
		t.Setenv("APP_ENV", "dev")
		// Whitespace, mixed case, a doubled comma and an empty token: the value
		// arrives however the deploy environment holds it, and the header must
		// carry clean lowercased addresses on the other side.
		t.Setenv("SES_REPLY_TO", " Support@JomForm.COM , ops@jomform.com ,, ")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"support@jomform.com", "ops@jomform.com"}
		if !reflect.DeepEqual(cfg.SESReplyTo, want) {
			t.Errorf("SESReplyTo = %#v, want %#v — SES_REPLY_TO no longer reaches the "+
				"sender, so every reply goes to the noreply@ address nobody reads", cfg.SESReplyTo, want)
		}
	})

	t.Run("unset means no header", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://localhost/jomform_test")
		t.Setenv("APP_ENV", "dev")
		// SES_FROM is set ON PURPOSE. The decision recorded at config.go is that
		// an unset SES_REPLY_TO yields NO Reply-To, never SES_FROM: a message
		// with no Reply-To sends the reply to From, which is
		// noreply@jomform.com — the black hole this variable exists to close.
		// Defaulting here would re-create the defect while looking configured.
		t.Setenv("SES_FROM", "noreply@jomform.com")
		t.Setenv("SES_REPLY_TO", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.SESReplyTo) != 0 {
			t.Errorf("SESReplyTo = %#v, want empty when SES_REPLY_TO is unset. An unset "+
				"variable must write NO Reply-To header — defaulting it to SES_FROM "+
				"(noreply@jomform.com) restores the silent-loss defect this feature closes", cfg.SESReplyTo)
		}
	})
}

// TestSESReplyToReachesEverySenderInMainGo pins the config -> SMTPConfig hop,
// the one no test covered. config.Load returning the right list and
// parseEmailList trimming it correctly together still prove nothing about
// whether `ReplyTo: cfg.SESReplyTo` is the field main.go assigns: the literal is
// valid Go without it, because ReplyTo is optional.
func TestSESReplyToReachesEverySenderInMainGo(t *testing.T) {
	src, err := os.ReadFile(mainGoPath)
	if err != nil {
		t.Fatalf("read %s: %v", mainGoPath, err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, mainGoPath, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", mainGoPath, err)
	}

	var found, missing []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isSMTPConfigType(lit.Type) {
			return true
		}
		pos := fset.Position(lit.Pos()).String()
		found = append(found, pos)
		if !hasReplyToCfgSESReplyTo(lit) {
			missing = append(missing, fmt.Sprintf("  %s\n%s", pos, indent(renderNode(fset, lit))))
		}
		return true
	})

	if len(found) < minSMTPSenderLiterals {
		t.Fatalf("%s builds %d email.SMTPConfig literal(s), this guard expects at least %d "+
			"(the auth mailer and the payments receipt sender). Fewer means the construction "+
			"moved and this guard is no longer checking the senders it claims to — teach it "+
			"the new shape rather than letting it pass vacuously. Found: %v",
			mainGoPath, len(found), minSMTPSenderLiterals, found)
	}
	if len(missing) > 0 {
		t.Errorf("%d of %d email.SMTPConfig literal(s) in %s do not set "+
			"`ReplyTo: cfg.SESReplyTo`, so the senders they build emit no Reply-To header "+
			"and a recipient's Reply is delivered to noreply@jomform.com, where nothing "+
			"reads it. Carry the monitored mailbox on EVERY sender:\n%s",
			len(missing), len(found), mainGoPath, strings.Join(missing, "\n"))
	}
	t.Logf("checked %d email.SMTPConfig literal(s) in %s: %s",
		len(found), mainGoPath, strings.Join(found, ", "))
}

// isSMTPConfigType reports whether a composite literal's type is email.SMTPConfig
// (a qualified selector) or a bare SMTPConfig (as written inside the email
// package itself).
func isSMTPConfigType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "SMTPConfig"
	case *ast.Ident:
		return t.Name == "SMTPConfig"
	}
	return false
}

// hasReplyToCfgSESReplyTo reports whether the literal carries
// `ReplyTo: cfg.SESReplyTo` exactly. Any other value — a literal list, another
// config field, a different variable — is not the wiring this guard exists to
// check, so it is reported rather than accepted.
func hasReplyToCfgSESReplyTo(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "ReplyTo" {
			continue
		}
		sel, ok := kv.Value.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok {
			continue
		}
		return x.Name == "cfg" && sel.Sel.Name == "SESReplyTo"
	}
	return false
}

// renderNode prints a node back to Go source for the failure message, so the
// reader sees the literal that is missing the field.
func renderNode(fset *token.FileSet, n ast.Node) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, n); err != nil {
		return fmt.Sprintf("<unprintable: %v>", err)
	}
	return buf.String()
}

// indent indents every line so a multi-line literal reads as one error entry.
func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}
