// Package db — unit tests for the migration-SQL view the extension guards read.

package db

import (
	"strings"
	"testing"
)

// TestDeclaredExtensionsIgnoresCommentsAndFunctionBodies is the guard for the
// parser the extension-placement guards and internal/testdb's repair both
// depend on.
//
// The defect this pins: a doc comment in 000001_init.up.sql reading
// "CREATE EXTENSION creates it in the FIRST schema…" was collected as a real
// declaration named `creates`, which silently corrupted the list — the repair
// then tried `CREATE EXTENSION creates IN public` and failed with
// `extension "creates" is not available` (SQLSTATE 0A000), and pgcrypto
// dropped out of the list entirely. Prose must never be read as DDL.
func TestDeclaredExtensionsIgnoresCommentsAndFunctionBodies(t *testing.T) {
	names, err := DeclaredExtensions()
	if err != nil {
		t.Fatalf("DeclaredExtensions: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("DeclaredExtensions = %v, want exactly the two declared extensions [pgcrypto citext]", names)
	}
	for i, want := range []string{"pgcrypto", "citext"} {
		if names[i] != want {
			t.Errorf("DeclaredExtensions[%d] = %q, want %q", i, names[i], want)
		}
	}
}

// TestStripSQLComments covers the shapes a migration can contain. The cases are
// chosen for the ways a naive stripper (or a naive regex) gets it wrong:
// a quote inside a comment, a comment inside a string, a doubled single quote
// inside a literal, a nested block comment, a dollar-quoted body carrying DDL
// text, and a $1 placeholder that is not a dollar quote.
func TestStripSQLComments(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{{
		name: "line comment",
		in:   "-- CREATE EXTENSION creates it\nCREATE EXTENSION citext SCHEMA public;",
		want: "\nCREATE EXTENSION citext SCHEMA public;",
	}, {
		name: "block comment",
		in:   "/* CREATE EXTENSION fake; */CREATE EXTENSION citext;",
		want: "CREATE EXTENSION citext;",
	}, {
		name: "nested block comment",
		in:   "/* outer /* CREATE EXTENSION fake; */ still */CREATE EXTENSION citext;",
		want: "CREATE EXTENSION citext;",
	}, {
		name: "string literal kept verbatim (the SCHEMA clause must survive)",
		in:   "CREATE EXTENSION citext SCHEMA 'public';",
		want: "CREATE EXTENSION citext SCHEMA 'public';",
	}, {
		name: "escaped quote inside a literal does not end it",
		in:   "SELECT 'it''s -- not a comment';",
		want: "SELECT 'it''s -- not a comment';",
	}, {
		name: "comment marker inside a literal is not a comment",
		in:   "INSERT INTO t VALUES ('-- CREATE EXTENSION fake');",
		want: "INSERT INTO t VALUES ('-- CREATE EXTENSION fake');",
	}, {
		name: "dollar-quoted function body is dropped",
		in:   "CREATE FUNCTION f() RETURNS void AS $$\nCREATE EXTENSION fake;\n$$ LANGUAGE plpgsql;",
		// The body is removed and the surrounding text is copied as-is, so the
		// newlines on either side of it become adjacent: "AS " then " LANGUAGE".
		want: "CREATE FUNCTION f() RETURNS void AS  LANGUAGE plpgsql;",
	}, {
		name: "tagged dollar quote",
		in:   "CREATE FUNCTION f() RETURNS void AS $body$ CREATE EXTENSION fake; $body$ LANGUAGE plpgsql;",
		want: "CREATE FUNCTION f() RETURNS void AS  LANGUAGE plpgsql;",
	}, {
		name: "positional placeholder is not a dollar quote",
		in:   "SELECT $1, $2;",
		want: "SELECT $1, $2;",
	}, {
		name: "unterminated dollar quote swallows the rest",
		in:   "CREATE EXTENSION citext SCHEMA public; CREATE FUNCTION f() AS $$ unfinished",
		want: "CREATE EXTENSION citext SCHEMA public; CREATE FUNCTION f() AS ",
	}, {
		name: "unterminated line comment at EOF",
		in:   "CREATE EXTENSION citext SCHEMA public; -- trailing",
		want: "CREATE EXTENSION citext SCHEMA public; ",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripSQLComments(tc.in); got != tc.want {
				t.Errorf("stripSQLComments(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestEveryDeclaredExtensionPinsPublicSchema is the static half of the
// placement guard, asserted here too so it runs without a database.
//
// It shares db's comment-aware view of the SQL, so a statement quoted in a
// comment is not mistaken for a declaration — and so that this check and the
// derivation that drives the repair cannot disagree about what a migration
// declares.
func TestEveryDeclaredExtensionPinsPublicSchema(t *testing.T) {
	migrations, err := MigrationStatements()
	if err != nil {
		t.Fatalf("MigrationStatements: %v", err)
	}
	seen := 0
	for _, m := range migrations {
		for _, match := range createExtensionRE.FindAllStringSubmatch(stripSQLComments(m.SQL), -1) {
			seen++
			if !strings.Contains(strings.ToUpper(match[2]), "SCHEMA PUBLIC") {
				t.Errorf("%s: CREATE EXTENSION %s does not pin `SCHEMA public` — a caller applying the "+
					"migrations with `SET search_path=<scratch>,public` would strand the extension in its "+
					"own schema (SQLSTATE 42704 for every other caller)", m.Name, match[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("no CREATE EXTENSION found in the migration set — this guard would pass vacuously")
	}
}
