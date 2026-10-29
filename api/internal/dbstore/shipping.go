// Managed shipping zones + rates (issue: shipping fee by zone).
//
// This is the STORE for the phase-1 shipping rate source: a workspace's zone
// sets, their zones (state / district / postcode-range rules) and the rate rows
// (price in sen, an optional weight band and an optional free-shipping
// threshold). The resolution RULES live in internal/shipping — this file only
// reads and writes rows, and hands the resolver the types it already speaks.
//
// SCOPING follows products/forms: rows carry account_id and every read takes the
// account SET the request resolved to ([]string), so a ?workspace= request sees
// the whole workspace and a single-account caller sees one account.
//
// THE THREE ROW TYPES ARE FLAT ON PURPOSE. `zoneSetSelect`, `zoneSelect` and
// `rateSelect` are registered in migration_scan_drift_test.go's selectConsts, and
// that guard aligns a select constant's column count with the EXPORTED FIELD
// count of the struct it scans into. Nested child slices (a set holding its zones)
// would break that alignment, so the row types mirror the tables one column per
// field and the parent/child assembly happens in Go.
package dbstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/shipping"
)

// ErrShippingSetNotFound: a PUT named a zone-set id the account does not own.
// It is a REFUSAL, not an upsert: a stale dashboard that lost the id must not
// silently mint a new set (every form pointing at the old one would keep
// resolving to nothing).
var ErrShippingSetNotFound = errors.New("set zon penghantaran tidak dijumpai")

// ShippingSetInUseError: a save tried to DELETE a zone set that a form still
// references. Deleting it would leave that form's shipping block resolving to
// nothing (a 400 at every checkout), so the refusal names the forms instead.
type ShippingSetInUseError struct {
	SetName string
	Forms   []string // form titles referencing the set
}

// ShippingSetInUseLeadIn is the literal every ShippingSetInUseError message
// contains. The REST edge keys the machine error CODE on this constant
// (httpapi/errcode.go's errCodeByPrefix), so the dashboard can translate the
// refusal into the merchant's own language; a re-wording here cannot silently
// detach the code from the sentence.
const ShippingSetInUseLeadIn = "sedang digunakan oleh borang"

func (e *ShippingSetInUseError) Error() string {
	if len(e.Forms) == 0 {
		return "set zon ini " + ShippingSetInUseLeadIn
	}
	return fmt.Sprintf("set zon %q %s: %s", e.SetName, ShippingSetInUseLeadIn, strings.Join(e.Forms, ", "))
}

// ShippingFormRef is a form that references a zone set (id + title).
type ShippingFormRef struct {
	ID    string
	Title string
}

// ShippingStore wraps pgx for shipping zone sets, zones and rates.
type ShippingStore struct{ pool *pgxpool.Pool }

// NewShippingStore builds a ShippingStore.
func NewShippingStore(pool *pgxpool.Pool) *ShippingStore { return &ShippingStore{pool: pool} }

// The column lists every read shares, one constant per table (and each
// registered in the migration/scan-drift guard, so a migration that adds a
// column cannot be half-applied to one call site only).
const (
	zoneSetSelect = `SELECT id, name, is_default FROM shipping_zone_sets`

	zoneSelect = `SELECT id, set_id, name, states, districts,
		COALESCE(postcode_from,''), COALESCE(postcode_to,''), priority, is_fallback,
		COALESCE(lat,''), COALESCE(lng,'')
		FROM shipping_zones`

	rateSelect = `SELECT id, zone_id, label, price_sen, min_weight_g, max_weight_g,
		free_over_sen, sort FROM shipping_rates`
)

// shippingZoneSetRow is one shipping_zone_sets row (header only).
type shippingZoneSetRow struct {
	ID        string
	Name      string
	IsDefault bool
}

// shippingZoneRow is one shipping_zones row: the match rules, no children.
type shippingZoneRow struct {
	ID           string
	SetID        string
	Name         string
	States       []string
	Districts    []string
	PostcodeFrom string
	PostcodeTo   string
	Priority     int
	IsFallback   bool
	// Lat / Lng are the zone's own point (phase 2): what a point-based courier
	// (Lalamove) is quoted to. Empty means unknown, which falls back to this
	// zone's rate rather than inventing a coordinate.
	Lat string
	Lng string
}

// shippingRateRow is one shipping_rates row.
type shippingRateRow struct {
	ID          string
	ZoneID      string
	Label       string
	PriceSen    int64
	MinWeightG  int
	MaxWeightG  int
	FreeOverSen int64
	Sort        int
}

// scanShippingZoneSetRow, scanShippingZoneRow and scanShippingRateRow are the
// three full-row scans the drift guard aligns against the constants above.
func scanShippingZoneSetRow(row pgx.Row) (*shippingZoneSetRow, error) {
	r := &shippingZoneSetRow{}
	if err := row.Scan(&r.ID, &r.Name, &r.IsDefault); err != nil {
		return nil, err
	}
	return r, nil
}

func scanShippingZoneRow(row pgx.Row) (*shippingZoneRow, error) {
	r := &shippingZoneRow{}
	if err := row.Scan(&r.ID, &r.SetID, &r.Name, &r.States, &r.Districts,
		&r.PostcodeFrom, &r.PostcodeTo, &r.Priority, &r.IsFallback,
		&r.Lat, &r.Lng); err != nil {
		return nil, err
	}
	return r, nil
}

func scanShippingRateRow(row pgx.Row) (*shippingRateRow, error) {
	r := &shippingRateRow{}
	if err := row.Scan(&r.ID, &r.ZoneID, &r.Label, &r.PriceSen, &r.MinWeightG,
		&r.MaxWeightG, &r.FreeOverSen, &r.Sort); err != nil {
		return nil, err
	}
	return r, nil
}

// Sets returns every zone set of the given accounts, with zones and rates,
// ordered default-first then by name. It is the whole read surface of the
// dashboard page and of the MCP tool.
func (s *ShippingStore) Sets(ctx context.Context, accountIDs []string) ([]shipping.ZoneSet, error) {
	if len(accountIDs) == 0 {
		return []shipping.ZoneSet{}, nil
	}
	return s.setsWhere(ctx, accountIDs, "")
}

// SetByID returns ONE zone set of the given accounts (found=false when the
// account owns no such set). The submit path uses it: a form's shipping block
// names a set id, and the resolution must see exactly that set.
func (s *ShippingStore) SetByID(ctx context.Context, accountIDs []string, setID string) (shipping.ZoneSet, bool, error) {
	if len(accountIDs) == 0 || strings.TrimSpace(setID) == "" {
		return shipping.ZoneSet{}, false, nil
	}
	sets, err := s.setsWhere(ctx, accountIDs, " AND id = $2", strings.TrimSpace(setID))
	if err != nil {
		return shipping.ZoneSet{}, false, err
	}
	if len(sets) == 0 {
		return shipping.ZoneSet{}, false, nil
	}
	return sets[0], true, nil
}

// setsWhere reads the sets (plus their zones and rates) for the account set,
// with an optional extra predicate on the set table (appended after the
// account filter, so the caller's placeholders start at $2).
func (s *ShippingStore) setsWhere(ctx context.Context, accountIDs []string, extra string, args ...any) ([]shipping.ZoneSet, error) {
	setArgs := []any{accountIDs}
	setArgs = append(setArgs, args...)

	rows, err := s.pool.Query(ctx,
		zoneSetSelect+` WHERE account_id = ANY($1)`+extra+` ORDER BY is_default DESC, lower(name), created_at`, setArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sets := []shipping.ZoneSet{}
	index := map[string]int{}
	for rows.Next() {
		row, serr := scanShippingZoneSetRow(rows)
		if serr != nil {
			return nil, serr
		}
		index[row.ID] = len(sets)
		sets = append(sets, shipping.ZoneSet{ID: row.ID, Name: row.Name, Default: row.IsDefault})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(sets) == 0 {
		return sets, nil
	}

	// Zones. zoneIndex maps a zone id to (set index, zone index) so a rate can be
	// attached without a second pass over the sets.
	zoneIndex := map[string][2]int{}
	zrows, err := s.pool.Query(ctx, zoneSelect+` WHERE account_id = ANY($1) ORDER BY priority, created_at`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer zrows.Close()
	for zrows.Next() {
		row, serr := scanShippingZoneRow(zrows)
		if serr != nil {
			return nil, serr
		}
		si, ok := index[row.SetID]
		if !ok {
			// Not in the account set we were asked about (a foreign account's
			// zone). Never surfaced.
			continue
		}
		sets[si].Zones = append(sets[si].Zones, shipping.Zone{
			ID:           row.ID,
			Name:         row.Name,
			States:       row.States,
			Districts:    row.Districts,
			PostcodeFrom: row.PostcodeFrom,
			PostcodeTo:   row.PostcodeTo,
			Priority:     row.Priority,
			Fallback:     row.IsFallback,
			Lat:          row.Lat,
			Lng:          row.Lng,
			Rates:        []shipping.Rate{},
		})
		zoneIndex[row.ID] = [2]int{si, len(sets[si].Zones) - 1}
	}
	if err := zrows.Err(); err != nil {
		return nil, err
	}

	rrows, err := s.pool.Query(ctx, rateSelect+` WHERE account_id = ANY($1) ORDER BY sort, price_sen, label`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rrows.Close()
	for rrows.Next() {
		row, serr := scanShippingRateRow(rrows)
		if serr != nil {
			return nil, serr
		}
		pos, ok := zoneIndex[row.ZoneID]
		if !ok {
			continue
		}
		si, zi := pos[0], pos[1]
		sets[si].Zones[zi].Rates = append(sets[si].Zones[zi].Rates, shipping.Rate{
			ID:          row.ID,
			Label:       row.Label,
			PriceSen:    row.PriceSen,
			MinWeightG:  row.MinWeightG,
			MaxWeightG:  row.MaxWeightG,
			FreeOverSen: row.FreeOverSen,
			Sort:        row.Sort,
		})
	}
	return sets, rrows.Err()
}

// Replace writes the account's WHOLE shipping configuration in ONE transaction:
// sets named in the payload are updated in place (their id — and therefore every
// form's reference to it — is preserved) and their zones/rates are rewritten,
// new sets are inserted, and sets the payload omits are DELETED (refused with
// ShippingSetInUseError when a form still points at them).
//
// The caller validates the shape (ValidateShippingSets) first: a set with no
// zone, an unnamed zone or a zone with no rate is refused there, so a save can
// never leave a form's block resolving to nothing.
//
// AN EMPTY LIST IS A VALID SAVE (card t_417fbacb). It deletes every set the
// payload omits, which is how a merchant removes their LAST set — and the loop
// below still refuses any of those deletes that a form's block points at, so the
// only sets an empty save can drop are ones nothing resolves against.
func (s *ShippingStore) Replace(ctx context.Context, accountID string, sets []shipping.ZoneSet) error {
	if strings.TrimSpace(accountID) == "" {
		return errors.New("shipping: account id required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	existing := map[string]string{} // id -> name
	rows, err := tx.Query(ctx, `SELECT id, name FROM shipping_zone_sets WHERE account_id=$1`, accountID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return err
		}
		existing[id] = name
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	kept := map[string]bool{}
	for _, set := range sets {
		setName := strings.TrimSpace(set.Name)
		id := strings.TrimSpace(set.ID)
		switch {
		case id == "":
			if err := tx.QueryRow(ctx,
				`INSERT INTO shipping_zone_sets (account_id, name, is_default) VALUES ($1,$2,$3) RETURNING id`,
				accountID, setName, set.Default).Scan(&id); err != nil {
				return err
			}
		case existing[id] != "":
			if _, err := tx.Exec(ctx,
				`UPDATE shipping_zone_sets SET name=$2, is_default=$3, updated_at=now() WHERE id=$1 AND account_id=$4`,
				id, setName, set.Default, accountID); err != nil {
				return err
			}
		default:
			return ErrShippingSetNotFound
		}
		kept[id] = true

		// Rewrite this set's zones + rates. Zone ids are regenerated: nothing
		// references a zone id across a save (the submit path matches by rule and
		// the sale snapshots the names it resolved).
		if _, err := tx.Exec(ctx, `DELETE FROM shipping_zones WHERE set_id=$1 AND account_id=$2`, id, accountID); err != nil {
			return err
		}
		for _, z := range set.Zones {
			var zoneID string
			if err := tx.QueryRow(ctx,
				`INSERT INTO shipping_zones (set_id, account_id, name, states, districts, postcode_from, postcode_to, priority, is_fallback, lat, lng)
				 VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9,$10,$11) RETURNING id`,
				id, accountID, strings.TrimSpace(z.Name), textArray(z.States), textArray(z.Districts),
				strings.TrimSpace(z.PostcodeFrom), strings.TrimSpace(z.PostcodeTo), z.Priority, z.Fallback,
				strings.TrimSpace(z.Lat), strings.TrimSpace(z.Lng)).Scan(&zoneID); err != nil {
				return err
			}
			for _, r := range z.Rates {
				if _, err := tx.Exec(ctx,
					`INSERT INTO shipping_rates (zone_id, account_id, label, price_sen, min_weight_g, max_weight_g, free_over_sen, sort)
					 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
					zoneID, accountID, strings.TrimSpace(r.Label), r.PriceSen, r.MinWeightG, r.MaxWeightG, r.FreeOverSen, r.Sort); err != nil {
					return err
				}
			}
		}
	}

	// Sets the payload dropped. A referenced set is a refusal, so a merchant
	// cannot break a live form by reorganising their zones.
	for id, name := range existing {
		if kept[id] {
			continue
		}
		refs, err := shippingSetForms(ctx, tx, accountID, id)
		if err != nil {
			return err
		}
		if len(refs) > 0 {
			return &ShippingSetInUseError{SetName: name, Forms: refs}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM shipping_zone_sets WHERE id=$1 AND account_id=$2`, id, accountID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// FormsUsingSet lists the forms of these accounts whose config points a shipping
// block at setID. The dashboard uses it to warn before a delete; Replace uses
// the same reading to refuse one.
func (s *ShippingStore) FormsUsingSet(ctx context.Context, accountIDs []string, setID string) ([]ShippingFormRef, error) {
	if len(accountIDs) == 0 || strings.TrimSpace(setID) == "" {
		return nil, nil
	}
	return shippingSetFormsByAccounts(ctx, s.pool, accountIDs, setID)
}

// FormsUsingZones counts the forms of these accounts that carry a shipping block
// whose rate source is the managed zone set (any set). The dashboard shows it
// beside the zone list: with 0, the configuration exists but nothing charges it.
func (s *ShippingStore) FormsUsingZones(ctx context.Context, accountIDs []string) (int, error) {
	if len(accountIDs) == 0 {
		return 0, nil
	}
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM forms
		  WHERE account_id = ANY($1)
		    AND deleted_at IS NULL
		    AND config @> jsonb_build_object('blocks', jsonb_build_array(jsonb_build_object('source', 'zones')))`,
		accountIDs).Scan(&n)
	return n, err
}

// shippingSetForms is the same reading inside a transaction (Replace).
func shippingSetForms(ctx context.Context, tx pgx.Tx, accountID, setID string) ([]string, error) {
	refs, err := shippingSetFormsByAccounts(ctx, tx, []string{accountID}, setID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Title)
	}
	return out, nil
}

// querier is the subset of pgx shared by a pool and a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// shippingSetFormsByAccounts reads the forms whose builder config carries a
// shipping block with this zone_set_id. The containment test is pure jsonb, so
// it reads the same fact the resolver does: `config.blocks[]` carrying that key.
func shippingSetFormsByAccounts(ctx context.Context, q querier, accountIDs []string, setID string) ([]ShippingFormRef, error) {
	rows, err := q.Query(ctx,
		`SELECT id, title FROM forms
		  WHERE account_id = ANY($1)
		    AND deleted_at IS NULL
		    AND config @> jsonb_build_object('blocks', jsonb_build_array(jsonb_build_object('zone_set_id', $2::text)))
		  ORDER BY lower(title)`, accountIDs, strings.TrimSpace(setID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShippingFormRef
	for rows.Next() {
		var r ShippingFormRef
		if err := rows.Scan(&r.ID, &r.Title); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ShippingInvalidLeadIn prefixes every ValidateShippingSets refusal on the wire.
// The REST edge keys the machine error CODE on this constant
// (httpapi/errcode.go's errCodeByPrefix → shipping.invalid), so the dashboard can
// render ITS OWN sentence in the merchant's language instead of the page's
// generic "Failed to save shipping rates" (card t_417fbacb). It lives here,
// beside the validation that writes it, so a re-wording cannot silently detach
// the code from the sentence — the same pairing ShippingSetInUseLeadIn uses.
const ShippingInvalidLeadIn = "kadar penghantaran tidak sah — "

// ValidateShippingSets refuses a configuration that cannot resolve: an unnamed
// set, a set with no zone, an unnamed zone, a zone with no rate, a negative
// price or an inverted weight band. It is deliberately strict — a save is a
// complete intent, and every shape it rejects would otherwise become a 400 for a
// BUYER at checkout, or (worse) a fee nobody intended.
//
// AN EMPTY LIST IS NOT ONE OF THOSE SHAPES (card t_417fbacb). Zero sets means
// the account charges no managed rates, which is the state the Shipping tab's
// own empty view already describes ("No zone set yet…"), and it is the only way
// a merchant can remove their LAST set: Replace deletes every set the payload
// omits. That delete is still safe because Replace REFUSES one that a form's
// shipping block points at (ShippingSetInUseError), so an empty save can only
// ever drop sets nothing resolves against.
//
// Every refusal it returns carries ShippingInvalidLeadIn.
func ValidateShippingSets(sets []shipping.ZoneSet) error {
	if err := validateShippingSets(sets); err != nil {
		return fmt.Errorf("%s%w", ShippingInvalidLeadIn, err)
	}
	return nil
}

// validateShippingSets is the rule set itself, without the wire lead-in, so the
// messages stay the shape the merchant has to fix.
func validateShippingSets(sets []shipping.ZoneSet) error {
	names := map[string]bool{}
	for _, set := range sets {
		name := strings.TrimSpace(set.Name)
		if name == "" {
			return errors.New("set zon perlu ada nama")
		}
		if names[strings.ToLower(name)] {
			return fmt.Errorf("nama set zon berganda: %s", name)
		}
		names[strings.ToLower(name)] = true
		if len(set.Zones) == 0 {
			return fmt.Errorf("set zon %q belum ada zon", name)
		}
		fallbacks := 0
		for _, z := range set.Zones {
			zn := strings.TrimSpace(z.Name)
			if zn == "" {
				return fmt.Errorf("set zon %q ada zon tanpa nama", name)
			}
			if z.Fallback {
				fallbacks++
			}
			if !z.Fallback && len(z.States) == 0 && len(z.Districts) == 0 &&
				strings.TrimSpace(z.PostcodeFrom) == "" {
				return fmt.Errorf("zon %q perlu sekurang-kurangnya satu peraturan (negeri, daerah atau poskod)", zn)
			}
			if strings.TrimSpace(z.PostcodeFrom) != "" || strings.TrimSpace(z.PostcodeTo) != "" {
				from, to := postcodeValue(z.PostcodeFrom), postcodeValue(z.PostcodeTo)
				if strings.TrimSpace(z.PostcodeFrom) != "" && from < 0 {
					return fmt.Errorf("zon %q: poskod mesti nombor", zn)
				}
				if strings.TrimSpace(z.PostcodeTo) != "" && to < 0 {
					return fmt.Errorf("zon %q: poskod mesti nombor", zn)
				}
				if from >= 0 && to >= 0 && to < from {
					return fmt.Errorf("zon %q: poskod akhir mesti lebih besar daripada poskod mula", zn)
				}
			}
			if len(z.Rates) == 0 {
				return fmt.Errorf("zon %q belum ada kadar", zn)
			}
			// A zone's own point (phase 2). Both coordinates or neither: a zone
			// with a latitude and no longitude would be quoted to a place that
			// does not exist, and 0/0 is a real point in the Atlantic.
			lat, lng := strings.TrimSpace(z.Lat), strings.TrimSpace(z.Lng)
			if (lat == "") != (lng == "") {
				return fmt.Errorf("zon %q: latitud dan longitud mesti diberi bersama", zn)
			}
			if lat != "" {
				if !validCoordinate(lat) || !validCoordinate(lng) {
					return fmt.Errorf("zon %q: latitud/longitud mesti nombor perpuluhan", zn)
				}
			}
			for _, r := range z.Rates {
				if r.PriceSen < 0 {
					return fmt.Errorf("zon %q: kadar tidak boleh negatif", zn)
				}
				if r.FreeOverSen < 0 {
					return fmt.Errorf("zon %q: ambang penghantaran percuma tidak boleh negatif", zn)
				}
				if r.MinWeightG < 0 || r.MaxWeightG < 0 {
					return fmt.Errorf("zon %q: berat tidak boleh negatif", zn)
				}
				if r.MaxWeightG > 0 && r.MaxWeightG < r.MinWeightG {
					return fmt.Errorf("zon %q: julat berat tidak sah", zn)
				}
			}
		}
		if fallbacks > 1 {
			return fmt.Errorf("set zon %q ada lebih daripada satu zon lalai", name)
		}
	}
	return nil
}

// validCoordinate reports whether s is a usable decimal coordinate: optional
// sign, digits, optional single fraction. No exponent, no units, no whitespace —
// the same shape a courier API accepts. Range is not checked here (a real
// provider validates the point it is given).
func validCoordinate(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if s[0] == '-' || s[0] == '+' {
		s = s[1:]
	}
	seenDigit, seenDot := false, false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			seenDigit = true
		case r == '.':
			if seenDot {
				return false
			}
			seenDot = true
		default:
			return false
		}
	}
	return seenDigit
}

// postcodeValue parses an all-digit postcode, or -1.
func postcodeValue(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return -1
	}
	var out int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		out = out*10 + int64(r-'0')
	}
	return out
}

// textArray normalises a string slice for a TEXT[] column: trimmed, non-empty,
// de-duplicated (case-insensitively) and never nil (an empty list, not NULL).
func textArray(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		k := strings.ToLower(v)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
	}
	return out
}
