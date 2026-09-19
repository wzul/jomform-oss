package dbstore

// Regression: every account_domains read was dead for any real row.
//
// `AddDomain` — the production write path for a new claim (dashboard
// POST /v1/rest/domains and the add_domain MCP tool) — inserts only
// (account_id, domain, provider). The five columns added later —
// auto_cname (000038), tls_error/tls_cert_enc/tls_key_enc (000043) and
// tls_email (000044) — are nullable with no default, so they are NULL on
// every such row. `ListDomains`/`GetDomain` scanned them into the plain
// `string` fields of AccountDomain, and a NULL cannot scan into a *string:
//
//	db: can't scan into dest[6] (col: auto_cname): cannot scan NULL into *string
//
// So the first real claim broke the whole list — MCP `list_domains` returned
// the error, and the dashboard's custom-domain list 500'd. The fix is
// COALESCE(col,'') in the shared full-row select; these tests pin that the
// writes path that produces NULLs is still able to read them back, and that
// the NULL-producing writes (SetTLSMode / SetTLSEmail) stay readable.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// domainFixture seeds user + account and returns the account id.
func domainFixture(t *testing.T, ctx context.Context, p *pgxpool.Pool) string {
	t.Helper()
	return uniqueAccount(t, ctx, p)
}

// TestDomainReadAfterProductionClaim reproduces the reported state exactly:
// a domain claimed through AddDomain and nothing else, then read back through
// BOTH full-row accessors. It asserts the row's five nullable columns really
// are NULL first — a fixture that cannot produce the reported state would
// certify the bug as absent — and then that the reads succeed and normalise
// NULL to "".
func TestDomainReadAfterProductionClaim(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := domainFixture(t, ctx, p)
	ds := NewDomainStore(p)

	const domain = "claim-read.example.test"
	d, err := ds.AddDomain(ctx, acc, domain, "cname")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}

	// The fixture must reproduce the reported state: the five nullable
	// columns are NULL on a row written by the production path.
	var autoCNAME, tlsError, tlsCert, tlsKey, tlsEmail *string
	if err := p.QueryRow(ctx,
		`SELECT auto_cname, tls_error, tls_cert_enc, tls_key_enc, tls_email
		   FROM account_domains WHERE id=$1`, d.ID).
		Scan(&autoCNAME, &tlsError, &tlsCert, &tlsKey, &tlsEmail); err != nil {
		t.Fatalf("read raw columns: %v", err)
	}
	if autoCNAME != nil || tlsError != nil || tlsCert != nil || tlsKey != nil || tlsEmail != nil {
		t.Fatalf("fixture no longer reproduces the bug: auto_cname=%v tls_error=%v tls_cert_enc=%v tls_key_enc=%v tls_email=%v (all must be NULL right after AddDomain)",
			autoCNAME, tlsError, tlsCert, tlsKey, tlsEmail)
	}

	rows, err := ds.ListDomains(ctx, acc)
	if err != nil {
		t.Fatalf("ListDomains after a production claim: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListDomains returned %d rows, want 1", len(rows))
	}
	got := rows[0]
	if got.ID != d.ID || got.Domain != domain || got.Provider != "cname" {
		t.Errorf("row identity = {%s %s %s}, want {%s %s cname}", got.ID, got.Domain, got.Provider, d.ID, domain)
	}
	if got.AutoCNAME != "" || got.TLSError != "" || got.TLSEmail != "" ||
		got.TLSCertEnc != "" || got.TLSKeyEnc != "" {
		t.Errorf("nullable columns did not normalise to empty strings: auto_cname=%q tls_error=%q tls_email=%q tls_cert_enc=%q tls_key_enc=%q",
			got.AutoCNAME, got.TLSError, got.TLSEmail, got.TLSCertEnc, got.TLSKeyEnc)
	}
	// tls_mode/tls_status are NOT NULL with defaults, so those values are real.
	if got.TLSMode != "auto" || got.TLSStatus != "" {
		t.Errorf("tls_mode=%q tls_status=%q, want auto/(empty)", got.TLSMode, got.TLSStatus)
	}
	if got.Verified {
		t.Error("a fresh claim must not read as verified")
	}

	// GetDomain is the same full-row shape and must survive the same row.
	gd, err := ds.GetDomain(ctx, acc, d.ID)
	if err != nil {
		t.Fatalf("GetDomain after a production claim: %v", err)
	}
	if gd.ID != d.ID || gd.AutoCNAME != "" || gd.TLSError != "" || gd.TLSEmail != "" {
		t.Errorf("GetDomain = {%s auto_cname=%q tls_error=%q tls_email=%q}, want the claim with empty nullable fields",
			gd.ID, gd.AutoCNAME, gd.TLSError, gd.TLSEmail)
	}
}

// TestDomainFullRowColumnBinding pins which database column lands in which
// struct field. The NULL→"" normalisation above makes every nullable field
// read as "" on a fresh claim, so a scan-destination swap between two of them
// is invisible to those assertions: the values are interchangeable when they
// are all empty. Here each column carries a DISTINCT value, so a swapped
// binding is caught even though every field still scans successfully.
func TestDomainFullRowColumnBinding(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := domainFixture(t, ctx, p)
	ds := NewDomainStore(p)

	const domain = "binding.example.test"
	d, err := ds.AddDomain(ctx, acc, domain, "cname")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	// Distinct sentinels, written straight to the columns so the assertion is
	// about the READ binding, not about the writers.
	if _, err := p.Exec(ctx,
		`UPDATE account_domains
		    SET auto_cname='binding-cname', tls_mode='custom', tls_status='binding-status',
		        tls_error='binding-error', tls_cert_enc='binding-cert',
		        tls_key_enc='binding-key', tls_email='binding-email'
		  WHERE id=$1`, d.ID); err != nil {
		t.Fatalf("seed distinct column values: %v", err)
	}

	want := AccountDomain{
		ID: d.ID, AccountID: acc, Domain: domain, Provider: "cname",
		AutoCNAME: "binding-cname", TLSMode: "custom", TLSStatus: "binding-status",
		TLSError: "binding-error", TLSCertEnc: "binding-cert",
		TLSKeyEnc: "binding-key", TLSEmail: "binding-email",
	}

	rows, err := ds.ListDomains(ctx, acc)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListDomains returned %d rows, want 1", len(rows))
	}
	got := *rows[0]
	want.CreatedAt = got.CreatedAt // the fixture does not control the timestamp
	if got != want {
		t.Errorf("ListDomains binding drift:\n got = %+v\nwant = %+v", got, want)
	}

	gd, err := ds.GetDomain(ctx, acc, d.ID)
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	if *gd != want {
		t.Errorf("GetDomain binding drift:\n got = %+v\nwant = %+v", *gd, want)
	}
}

// TestDomainReadAfterNullWritingUpdates covers the other NULL producers: the
// TLS mode switch deliberately clears the stored custom cert + error, and
// SetTLSEmail clears an empty address, so a row that HAS been configured still
// goes back to NULL through the production write paths. Both writes must leave
// the row readable.
func TestDomainReadAfterNullWritingUpdates(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := domainFixture(t, ctx, p)
	ds := NewDomainStore(p)

	const domain = "tls-null.example.test"
	if _, err := ds.AddDomain(ctx, acc, domain, "cname"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}

	// Configure: a custom cert + a per-domain ACME email (both encrypted
	// blobs in production; the store only moves strings).
	if err := ds.SetCustomCert(ctx, acc, domain, "enc-cert", "enc-key"); err != nil {
		t.Fatalf("SetCustomCert: %v", err)
	}
	if err := ds.SetTLSEmail(ctx, acc, domain, "tls@example.test"); err != nil {
		t.Fatalf("SetTLSEmail: %v", err)
	}
	rows, err := ds.ListDomains(ctx, acc)
	if err != nil {
		t.Fatalf("ListDomains with a configured cert: %v", err)
	}
	if len(rows) != 1 || rows[0].TLSCertEnc != "enc-cert" || rows[0].TLSKeyEnc != "enc-key" ||
		rows[0].TLSEmail != "tls@example.test" || rows[0].TLSMode != "custom" {
		t.Fatalf("configured row not read back: %+v", rows)
	}

	// Switching back to auto clears the cert + error to NULL.
	if err := ds.SetTLSMode(ctx, acc, domain, "auto"); err != nil {
		t.Fatalf("SetTLSMode: %v", err)
	}
	// Clearing the ACME email writes NULL.
	if err := ds.SetTLSEmail(ctx, acc, domain, ""); err != nil {
		t.Fatalf("SetTLSEmail(clear): %v", err)
	}
	rows, err = ds.ListDomains(ctx, acc)
	if err != nil {
		t.Fatalf("ListDomains after the TLS writes cleared the columns: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListDomains returned %d rows, want 1", len(rows))
	}
	if got := rows[0]; got.TLSMode != "auto" || got.TLSCertEnc != "" || got.TLSKeyEnc != "" ||
		got.TLSError != "" || got.TLSEmail != "" {
		t.Errorf("cleared row = {mode=%q cert=%q key=%q err=%q email=%q}, want auto and empty",
			got.TLSMode, got.TLSCertEnc, got.TLSKeyEnc, got.TLSError, got.TLSEmail)
	}
}

// TestDomainNarrowReadsUnaffected pins the two accessors that select only
// non-nullable columns (ByDomain, GetByID) plus the auto-CNAME status write:
// they carry no nullable column, so the class fix is limited to the two
// full-row selects and these must stay working.
func TestDomainNarrowReadsUnaffected(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := domainFixture(t, ctx, p)
	ds := NewDomainStore(p)

	const domain = "narrow.example.test"
	d, err := ds.AddDomain(ctx, acc, domain, "cname")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if err := ds.SetAutoCNAME(ctx, acc, domain, "created"); err != nil {
		t.Fatalf("SetAutoCNAME: %v", err)
	}
	if err := ds.MarkVerified(ctx, acc, domain); err != nil {
		t.Fatalf("MarkVerified: %v", err)
	}

	if got, err := ds.ByDomain(ctx, domain); err != nil || got == nil || got.ID != d.ID || !got.Verified {
		t.Errorf("ByDomain = %+v, %v; want the verified claim", got, err)
	}
	if got, err := ds.GetByID(ctx, acc, d.ID); err != nil || got == nil || got.Domain != domain {
		t.Errorf("GetByID = %+v, %v; want the claim", got, err)
	}
	// And the full-row read now reports the recorded status.
	rows, err := ds.ListDomains(ctx, acc)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(rows) != 1 || rows[0].AutoCNAME != "created" || !rows[0].Verified {
		t.Errorf("full row = %+v, want auto_cname=created verified=true", rows)
	}
}
