// Command media-reconcile reconciles the media bucket against the `media` table
// and reports (and, with -apply, repairs) the bytes each account is charged for.
//
// REPORT MODE IS THE DEFAULT AND IS READ-ONLY. It opens Postgres with
// default_transaction_read_only=on and only ever lists the bucket, so it cannot
// change a row, an object or a counter. -apply performs exactly two kinds of
// write, both of them named in its output:
//
//  1. delete the object keys of groups EXPLICITLY classified deletable by a
//     decisions file, after re-checking every reference surface at apply time;
//  2. set accounts.media_used_bytes to the SUM of object sizes actually under
//     that account's prefix, re-listed after the deletes (never old - size).
//
// Usage:
//
//	media-reconcile -dsn "$DATABASE_URL" -endpoint "$S3_ENDPOINT" \
//	  -access-key "$S3_ACCESS_KEY" -secret-key "$S3_SECRET_KEY" \
//	  -bucket "$S3_BUCKET" -decisions decisions.json -json report.json
//
// Every flag falls back to the API's own environment variable, so the tool can be
// run beside the API with the same config.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/wzul/jomform/api/internal/mediareconcile"
)

func main() {
	var (
		dsn         = flag.String("dsn", os.Getenv("DATABASE_URL"), "Postgres DSN (default $DATABASE_URL)")
		endpoint    = flag.String("endpoint", os.Getenv("S3_ENDPOINT"), "S3/MinIO endpoint (default $S3_ENDPOINT)")
		accessKey   = flag.String("access-key", os.Getenv("S3_ACCESS_KEY"), "S3 access key (default $S3_ACCESS_KEY)")
		secretKey   = flag.String("secret-key", os.Getenv("S3_SECRET_KEY"), "S3 secret key (default $S3_SECRET_KEY)")
		bucketName  = flag.String("bucket", os.Getenv("S3_BUCKET"), "bucket name (default $S3_BUCKET)")
		region      = flag.String("region", envOr("S3_REGION", "us-east-1"), "S3 region")
		prefix      = flag.String("prefix", "u/", "key prefix to reconcile")
		publicBase  = flag.String("public-base", os.Getenv("S3_PUBLIC_BASE"), "public media base for adopted row URLs")
		decisionsFP = flag.String("decisions", "", "JSON decisions file (required with -apply)")
		jsonOut     = flag.String("json", "", "write the machine-readable report to this path")
		apply       = flag.Bool("apply", false, "delete classified-deletable objects and recompute counters from the store")
	)
	flag.Parse()

	if *dsn == "" {
		fail("-dsn or $DATABASE_URL is required")
	}
	if *bucketName == "" {
		fail("-bucket or $S3_BUCKET is required")
	}
	if *apply && *decisionsFP == "" {
		fail("-apply needs -decisions: deletion requires an explicit ruling per group (there is no default delete)")
	}
	ctx := context.Background()

	store, err := mediareconcile.NewPGStore(ctx, *dsn, !*apply)
	if err != nil {
		fail("%v", err)
	}
	defer store.Close()
	if !*apply {
		ro, err := store.ReadOnlySession(ctx)
		if err != nil {
			fail("read-only check: %v", err)
		}
		if !ro {
			fail("refusing to continue: database session is not read-only in report mode")
		}
		if err := store.AssertReadOnly(ctx); err != nil {
			fail("%v", err)
		}
	}

	bucket, err := mediareconcile.NewS3Bucket(*endpoint, *accessKey, *secretKey, *bucketName, *region)
	if err != nil {
		fail("%v", err)
	}

	objects, err := bucket.List(ctx, *prefix)
	if err != nil {
		fail("%v", err)
	}
	rows, err := store.MediaRows(ctx)
	if err != nil {
		fail("%v", err)
	}
	accounts, err := store.Accounts(ctx)
	if err != nil {
		fail("%v", err)
	}
	groups, unparsed := mediareconcile.BuildGroups(objects, rows)

	// References are resolved for EVERY group, not only the row-less ones. The
	// card only needs them for row-less groups, but a rowed group printed as
	// "refs: none" reads as "unreferenced" when it means "not scanned" — and a
	// rowed object can be referenced too (the pre-#545 row in prod is named by
	// both a form and a product).
	uuids := make([]string, 0, len(groups))
	for _, g := range groups {
		uuids = append(uuids, g.Base)
	}
	refs, err := store.References(ctx, uuids)
	if err != nil {
		fail("%v", err)
	}
	for _, g := range groups {
		if hits := refs[g.Base]; len(hits) > 0 {
			g.Refs = hits
		}
	}

	var decisions map[string]mediareconcile.Decision
	if *decisionsFP != "" {
		data, err := os.ReadFile(*decisionsFP)
		if err != nil {
			fail("read decisions: %v", err)
		}
		decisions, err = mediareconcile.ParseDecisions(data, groups)
		if err != nil {
			fail("%v", err)
		}
	}

	report := mediareconcile.BuildReport(*bucketName, *prefix, objects, rows, groups, unparsed, accounts, decisions)
	report.ReadOnly = !*apply
	report.Render(os.Stdout)
	if *jsonOut != "" {
		if err := writeJSON(*jsonOut, report); err != nil {
			fail("write json: %v", err)
		}
		fmt.Printf("\nwrote %s\n", *jsonOut)
	}

	if !*apply {
		fmt.Printf("\nscanned reference surfaces: %s\n", strings.Join(mediareconcile.Surfaces(), ", "))
		return
	}

	if err := applyChanges(ctx, store, bucket, groups, refs, *prefix, *publicBase, *bucketName); err != nil {
		fail("apply: %v", err)
	}
}

// applyChanges performs the two writes -apply is allowed to make, in the order
// that cannot lose bytes: re-verify references, delete only approved keys,
// confirm they are gone, then derive the counter from the re-listed store.
func applyChanges(ctx context.Context, store *mediareconcile.PGStore, bucket mediareconcile.Bucket,
	groups []*mediareconcile.Group, refs map[string][]mediareconcile.RefHit,
	prefix, publicBase, bucketName string) error {

	// 1. Re-check references for every group a decision wants deleted. The
	//    report may be minutes old; a product edited in between must win.
	var recheck []string
	wanted := map[string]bool{}
	for _, g := range groups {
		if g.Decision == mediareconcile.DecisionDeletable {
			recheck = append(recheck, g.Base)
			wanted[g.AccountID+"/"+g.Base] = true
		}
	}
	if len(recheck) > 0 {
		fresh, err := store.References(ctx, recheck)
		if err != nil {
			return err
		}
		if err := mediareconcile.VerifyDeletable(groups, fresh); err != nil {
			return err
		}
		for _, g := range groups {
			if wanted[g.AccountID+"/"+g.Base] {
				g.Refs = nil
			}
		}
	}

	keys, err := mediareconcile.DeletableKeys(groups)
	if err != nil {
		return err
	}

	// 2. Adopt first: an adopted row is additive metadata, and doing it before
	//    the deletes keeps the two lists independent.
	adopted := mediareconcile.AdoptRows(groups, publicBase)
	for _, row := range adopted {
		if _, err := store.CreateMediaRow(ctx, row); err != nil {
			return err
		}
		fmt.Printf("adopted row %s size=%d (%s)\n", row.Key, row.Size, row.AccountID)
	}

	// 3. Delete by object key, then confirm absence rather than trusting the
	//    delete call's exit status.
	for _, k := range keys {
		fmt.Printf("delete object %s\n", k)
	}
	if len(keys) > 0 {
		if err := bucket.Delete(ctx, keys); err != nil {
			return err
		}
		for _, k := range keys {
			present, err := bucket.Exists(ctx, k)
			if err != nil {
				return err
			}
			if present {
				return fmt.Errorf("object %s still present after delete", k)
			}
		}
		fmt.Printf("verified absent: %d object(s)\n", len(keys))
	}

	// 4. Counter := SUM(object sizes) from a FRESH full listing, per account,
	//    clamped to the cap. This is the only arithmetic that can repair a
	//    counter that never matched either rows or store.
	after, err := bucket.List(ctx, prefix)
	if err != nil {
		return err
	}
	storeBytes := map[string]int64{}
	for _, o := range after {
		if a := mediareconcile.AccountFromKey(o.Key); a != "" {
			storeBytes[a] += o.Size
		}
	}
	accounts, err := store.Accounts(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("\nquota recompute (%s)\n", mediareconcile.QuotaMethod)
	for _, a := range accounts {
		next := storeBytes[a.ID]
		if next > mediareconcile.MaxWorkspaceBytes {
			next = mediareconcile.MaxWorkspaceBytes
		}
		before, err := store.SetMediaUsedBytes(ctx, a.ID, next)
		if err != nil {
			return err
		}
		if before != next {
			fmt.Printf("  %s %-24s %d -> %d\n", a.ID, a.Name, before, next)
		}
	}
	for acc := range storeBytes {
		found := false
		for _, a := range accounts {
			if a.ID == acc {
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("  %s: objects in bucket but no accounts row - cannot be charged\n", acc)
		}
	}
	fmt.Printf("\napply complete: bucket=%s objects_after=%d adopted_rows=%d deleted_objects=%d\n",
		bucketName, len(after), len(adopted), len(keys))
	return nil
}

func writeJSON(path string, report *mediareconcile.Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "media-reconcile: "+format+"\n", args...)
	os.Exit(1)
}
