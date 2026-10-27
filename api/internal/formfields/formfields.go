// Package formfields holds the strict field-registration contract for
// MCP-driven form creation (issue #254). A merchant's AI agent registers each
// input field via MCP; JomForm validates the contract, renders the inputs, and
// re-validates all fields server-side on submit (agent JS is client-side only
// and bypassable). The contract lives here so both the MCP layer (mcpsrv) and
// the public submit handler (httpapi) share one definition — no drift.
package formfields

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/wzul/jomform/api/internal/auth"
)

// Choice is one option of a select/radio/checkbox/image-choice field. It is
// only populated when a contract is derived from a form's builder blocks
// (Contract) so the menu checkout can render the merchant's own choices;
// registered_fields may also carry choices.
type Choice struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Field is the strict field-registration contract (issue #254). JomForm
// validates it and renders the inputs; the agent's HTML must match the
// registered FieldID or the submission won't record correctly.
//
// Issue #493: Placeholder / DefaultValue / Choices are optional render hints
// carried through from the form's builder blocks so the restaurant-menu
// checkout can render an attached form's fields with the merchant's own
// labels, placeholders and choices. They never affect validation.
type Field struct {
	FieldID    string `json:"field_id"`
	Type       string `json:"type"`
	Label      string `json:"label"`
	Required   bool   `json:"required"`
	Validation any    `json:"validation,omitempty"` // string pattern or object
	ProductID  string `json:"product_id,omitempty"`
	// Issue #493: render hints (menu checkout extra fields).
	Placeholder  string   `json:"placeholder,omitempty"`
	DefaultValue string   `json:"default_value,omitempty"`
	Choices      []Choice `json:"choices,omitempty"`
	// Condition is the block's conditional-logic rule, when the contract was
	// derived from builder blocks (Contract). A field whose condition is false
	// for a submission is NOT part of it: not required, not validated, and its
	// answer is dropped (VisibleAnswers) — the buyer never saw the field.
	Condition *Condition `json:"condition,omitempty"`
}

// Condition is a block's conditional-logic rule (the builder's "conditional
// logic" panel): the block is shown only while the answer to FieldID satisfies
// Op. It is ONE definition, shared by the public renderer (which emits it as
// the block's `data-condition` attribute), the browser script that hides the
// block, and this package's submit-side re-evaluation — so the page and the
// server cannot disagree about whether a block was visible.
type Condition struct {
	FieldID string `json:"field_id"`
	Op      string `json:"op"` // equals | not_equals | contains | gt | lt | not_empty
	Value   string `json:"value"`
}

// Met reports whether the condition holds for the given answers. answers maps
// field id → submitted value; nums maps field id → that value's numeric form
// for gt/lt (a nil map is allowed and reads as 0 — that is what the callers
// without numbers, e.g. the CHIP feed router, have always passed).
//
// An unknown (or empty) Op matches: a rule the server cannot parse must never
// hide a field the buyer could see.
func (c *Condition) Met(answers map[string]string, nums map[string]int64) bool {
	if c == nil {
		return true
	}
	v := strings.TrimSpace(answers[c.FieldID])
	switch c.Op {
	case "equals":
		if c.Value == "" {
			return v != ""
		}
		return strings.EqualFold(v, strings.TrimSpace(c.Value))
	case "not_equals":
		cc := *c
		cc.Op = "equals"
		return !cc.Met(answers, nums)
	case "contains":
		return strings.Contains(strings.ToLower(v), strings.ToLower(strings.TrimSpace(c.Value)))
	case "gt":
		return nums[c.FieldID] > parseNum(c.Value)
	case "lt":
		return nums[c.FieldID] < parseNum(c.Value)
	case "not_empty":
		return v != ""
	}
	return true
}

// parseNum reads the numeric side of a gt/lt condition. A value that is not an
// integer reads as 0, which is the historical behaviour of the comparison.
func parseNum(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// intAnswers is the numeric view of the answers that gt/lt compare against.
func intAnswers(answers map[string]string) map[string]int64 {
	nums := make(map[string]int64, len(answers))
	for k, v := range answers {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			nums[k] = n
		}
	}
	return nums
}

// ValidTypes is the allowlist of input field types JomForm renders and
// records. The agent cannot invent a type — JomForm keeps authority over
// field types (issue #254).
//
// Card t_7fb80479: `text` is in this list because it IS an input field. The
// builder's palette places "Text / label" in the Standard group beside
// textarea/number/select with placeholder / max length / default value /
// pattern — settings that only mean something for a control — and BOTH public
// renderers paint it as `<input type="text">` whose label is the block's value
// (payment renderer since t_23289ce3, response renderer since t_99c3a08b).
// Prose stays available as `section`/`html`, which are display blocks and not
// field types. The block had two meanings before this card: response mode
// painted a control while the payment renderer painted `<p class="text">`, and
// the lists below excluded it, so an agent could not register the one field
// the palette offers for a short answer.
var ValidTypes = map[string]bool{
	"name": true, "email": true, "phone": true, "textarea": true,
	"text":   true,
	"number": true, "select": true, "radio": true, "checkbox": true,
	"date": true, "time": true, "website": true, "address": true,
	"consent": true, "hidden": true, "list": true, "repeater": true,
	"image-choice": true, "captcha": true,
}

// fieldIDUnsafe matches any character that is not safe in a field_id.
var fieldIDUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// ValidateContract validates a field-registration contract. Returns an error
// describing the first violation. field_id, type and label are required; type
// must be a known input type; field_ids must be unique and URL-safe.
func ValidateContract(fields []Field) error {
	seen := map[string]bool{}
	for i := range fields {
		fl := &fields[i]
		fl.FieldID = strings.TrimSpace(fl.FieldID)
		fl.Label = strings.TrimSpace(fl.Label)
		if fl.FieldID == "" {
			return fmt.Errorf("medan #%d: field_id wajib", i+1)
		}
		if fieldIDUnsafe.MatchString(fl.FieldID) {
			return fmt.Errorf("medan #%d: field_id tidak sah (hanya a-z, 0-9, _ , -)", i+1)
		}
		if seen[fl.FieldID] {
			return fmt.Errorf("medan #%d: field_id %q didaftar dua kali", i+1, fl.FieldID)
		}
		seen[fl.FieldID] = true
		if fl.Type == "" {
			return fmt.Errorf("medan #%d: type wajib", i+1)
		}
		if !ValidTypes[fl.Type] {
			return fmt.Errorf("medan #%d: type %q tidak sah", i+1, fl.Type)
		}
		if fl.Label == "" {
			return fmt.Errorf("medan #%d: label wajib", i+1)
		}
	}
	return nil
}

// FromConfig reads a form's registered input fields (issue #254) from its
// config JSON. Returns nil when none are registered.
func FromConfig(config map[string]any) []Field {
	if config == nil {
		return nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil
	}
	var cfg struct {
		RegisteredFields []Field `json:"registered_fields"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	return cfg.RegisteredFields
}

// Validate re-validates submitted field values against the form's field
// contract (issue #254). Agent JS is client-side only and bypassable, so the
// server re-validates every field. Returns an error describing the first
// violation. A form with no contract is not enforced.
//
// The contract is derived with Contract — i.e. the SAME rule the menu checkout
// validates against: an explicit registered_fields contract (issue #254) wins,
// otherwise it comes from the form's own builder blocks (#493). Deriving from
// the blocks here is the point (card t_589dc006): this used to read
// registered_fields alone, so a form whose required markers live in
// config.blocks[].required — the shape every builder-made form has — had
// NOTHING enforced on its own submit route. A direct POST of {} then stored an
// empty submission and fired the merchant's notification email.
func Validate(config map[string]any, fields map[string]string) error {
	return ValidateFields(Contract(config), fields)
}

// ValidateFields re-validates submitted values against an explicit field
// contract (issue #493). The restaurant-menu checkout validates against the
// contract derived from the attached form's builder blocks, so the same
// enforcement applies without a registered_fields entry.
func ValidateFields(regs []Field, fields map[string]string) error {
	if len(regs) == 0 {
		return nil // no contract registered — nothing to enforce
	}
	nums := intAnswers(fields)
	for _, r := range regs {
		// Card t_9e875bca: a block whose conditional-logic rule is false for
		// THIS submission was hidden on the page the buyer filled in, so its own
		// `required` marker (and any pattern/type check) does not apply — the
		// buyer could not have filled it. Without this, a form with a required
		// conditional field rejected every submission made while the field was
		// hidden. The value itself is dropped by VisibleAnswers before it is
		// stored, so there is nothing left to type-check either.
		if r.Condition != nil && !r.Condition.Met(fields, nums) {
			continue
		}
		val, present := fields[r.FieldID]
		if r.Required {
			if !present || strings.TrimSpace(val) == "" {
				return fmt.Errorf("medan wajib tidak diisi: %s", r.Label)
			}
		}
		if !present || strings.TrimSpace(val) == "" {
			continue
		}
		switch r.Type {
		case "email":
			if !auth.ValidEmail(val) {
				return fmt.Errorf("emel tidak sah: %s", r.Label)
			}
		case "website":
			if !ValidWebsiteURL(val) {
				return fmt.Errorf("URL tidak sah: %s", r.Label)
			}
		case "number":
			if _, err := strconv.ParseFloat(val, 64); err != nil {
				return fmt.Errorf("nombor tidak sah: %s", r.Label)
			}
		}
		// Optional per-field validation pattern (string form).
		if pat, ok := r.Validation.(string); ok && pat != "" {
			re, err := regexp.Compile(pat)
			if err == nil && !re.MatchString(val) {
				return fmt.Errorf("nilai tidak sah: %s", r.Label)
			}
		}
	}
	return nil
}

// ValidWebsiteURL reports whether val is an absolute URL of the kind a
// browser's own `<input type="url">` accepts.
//
// THE RULE IS THE BROWSER'S, NOT "http(s) only". HTML defines the url state's
// constraint as "while the value is neither the empty string nor a valid
// absolute URL, the element is suffering from a type mismatch"
// (https://html.spec.whatwg.org/multipage/input.html#url-state-(type=url)), and
// every engine implements that as "the URL parser returned a URL" (see
// whatwg/html#11083). Measured in Chrome 145 against the public form:
//
//	ftp://example.com/x   valid      example.com        invalid (no scheme)
//	https://example.com/x valid      http://            invalid (no host)
//	mailto:a@b            valid      ftp://             invalid (no host)
//	ws://example.com      valid      //example.com/x    invalid (no scheme)
//	file:///etc/passwd    valid      tel:+60123         valid
//
// The check used to be `strings.HasPrefix(val, "http://") ||
// strings.HasPrefix(val, "https://")`, so a buyer could type a value the page
// itself declared valid (`ftp://example.com/x`) and have the POST refused with
// `URL tidak sah` — the exact divergence the server-side contract exists to
// close.
//
// Three places where this is not literally "whatever the page accepted", all
// in the strict direction and all documented here because a reader will look
// for them:
//
//   - a network scheme (http, https, ftp, ws, wss) with an EMPTY host is
//     refused — the browser refuses those too (`http://` is a type mismatch),
//     so this is the same rule, not a deviation;
//   - the script-bearing opaque schemes javascript:, data: and vbscript: are
//     refused although the browser accepts them. An answer is stored and then
//     read OUTSIDE the form (the Sales answer list, the CSV export), so the
//     server does not record an executable URI into a merchant's data even
//     though the string would satisfy the page's own check;
//   - Go's parser is stricter than the WHATWG URL parser on two malformed
//     shapes — `http:/example.com` (one slash) and a host containing a space.
//     Both are refused here and accepted by a browser, and that is fine: this
//     is not a second, hand-written URL parser, and a value that malformed is
//     worth a corrective message rather than a stored broken link.
//
// The second and third are the only places the server is stricter than the
// page; the test pins all three.
func ValidWebsiteURL(val string) bool {
	v := strings.TrimSpace(val)
	if v == "" {
		return false
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" {
		// No scheme at all (`jomform.com`, `//example.com/x`) is the value the
		// browser also refuses — the mistake this check exists to catch.
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "javascript", "data", "vbscript":
		return false
	}
	if u.Host == "" && networkScheme(u.Scheme) {
		return false
	}
	return true
}

// networkScheme reports whether s is a scheme whose URL has an authority, so a
// value without a host is a broken link (and the browser's parser refuses it).
func networkScheme(s string) bool {
	switch strings.ToLower(s) {
	case "http", "https", "ftp", "ws", "wss":
		return true
	}
	return false
}

// collectableTypes is the allowlist of builder-block types that collect a
// value: the blocks a renderer paints as an answer control keyed by the block
// id. Display/pricing blocks (image, divider, section, html, product, quantity,
// option, shipping, total) are never part of a field contract, and the two
// withdrawn block types (`file`, `coupon`) print a note instead of a control.
//
// "Mirrors the renderer's input set" is not a claim any more — it is checked.
// httpapi/contract_renderer_parity_test.go drives both public renderers and the
// menu checkout with one block of every palette type and fails when the two
// answers disagree: a block that paints an answer control but is missing here is
// a field the menu checkout never renders and never validates (that is exactly
// how `text` went missing, card t_4a466bc5), and an entry with no control asks
// the buyer for something they cannot see. Add a renderer arm and its entry in
// the same change, or the build says so.
//
// Card t_7fb80479: `text` IS collectable, because the renderers paint it as a
// text input. Leaving it out (its old reading — prose) meant a required text
// field was never enforced server-side: the panel's `required` was an HTML
// attribute a direct POST could ignore, and Validate had nothing to check
// against. `section`/`html` remain display blocks and stay out.
var collectableTypes = map[string]bool{
	"name": true, "email": true, "phone": true, "textarea": true,
	"text":   true,
	"number": true, "select": true, "radio": true, "checkbox": true,
	"date": true, "time": true, "website": true, "address": true,
	"consent": true, "hidden": true, "list": true, "repeater": true,
	"image-choice": true, "captcha": true,
}

// proseBlockTypes are the block types that carry display copy instead of an
// answer: the intro line a form opens with. `html` is escaped exactly like
// `section` in both renderers, so both are prose.
var proseBlockTypes = map[string]bool{"section": true, "html": true}

// blocksOf decodes a form config's builder blocks. Returns nil for a config
// with no blocks, or one this package cannot read — every reader here reports
// only what it recognises.
func blocksOf(config map[string]any) []block {
	if config == nil {
		return nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil
	}
	var cfg struct {
		Blocks []block `json:"blocks"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	return cfg.Blocks
}

// Description returns the sentence that introduces a form — its intro line.
//
// Card t_7fb80479: the description is read from the form's PROSE, never from a
// field's label. Three places derive it (the template gallery, MCP
// list_templates, and the builder's save) and all three read the first `text`
// block's value — which was right while `text` meant prose, and is wrong now
// that `text` is an input whose value is its LABEL: a merchant's
// name-collecting form described itself as "Cuba. Nama" instead of its intro.
//
// The first `section`/`html` block with a value wins, wherever the merchant
// placed it. A form written under the old content model — the seeded template
// library wrote its intro into a `text` block, and those rows are migrated by
// card t_d46c21e0 — falls back to its first `text` block, so its gallery card
// keeps the description it has today on either side of that migration.
func Description(config map[string]any) string {
	var legacy string
	for _, b := range blocksOf(config) {
		v := strings.TrimSpace(b.Value)
		if v == "" {
			continue
		}
		if proseBlockTypes[b.Type] {
			return v
		}
		if b.Type == "text" && legacy == "" {
			legacy = v
		}
	}
	return legacy
}

// block is the subset of a builder block the contract derivation reads.
type block struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	Value        string     `json:"value"`
	Required     bool       `json:"required"`
	Condition    *Condition `json:"condition"`
	Placeholder  string     `json:"placeholder"`
	DefaultValue string     `json:"default_value"`
	Choices      []struct {
		Label string `json:"label"`
		Value string `json:"value"`
	} `json:"choices"`
}

// SubmitKeys returns, for each block of a form's config.blocks array in block
// order, the key that block's answer is submitted under.
//
// A block WITH an id keeps its trimmed id — unchanged. A block WITHOUT one gets
// a key synthesised from its POSITION: "_block_<index>". Card t_e854b64c: the
// contract and both public renderers used to key such a block by its id, so
// the contract (which skips an empty id) carried nothing for it while the pages
// still painted a control whose key was "" — a required marker on one of those
// legacy blocks was an HTML attribute a direct POST could ignore, the #254
// bypass class again. Measured on prod: 6 required id-less blocks on 4
// published forms, where POSTing no field values got past the contract.
//
// Deriving the key HERE, in the ONE function the contract and both renderers
// call, is what makes "the key the server validates" and "the key the page
// submits" the same value rather than two rules that happen to pair up. The
// stored config is not rewritten, so a legacy form needs no re-save and no data
// migration, and a page rendered before this change and a submit handled after
// it cannot disagree about a form whose blocks carry no id: both derive
// "_block_<index>" from the same array.
//
// The key must be STABLE for a given config — it is interpolated into the
// page's data-id/name, collected by the page's own script, re-derived on the
// submit side and stored on the submission — so it comes from the index alone,
// never from a random or per-request value. It must also be UNIQUE: a config
// may carry a real block whose id is literally "_block_2", so a synthetic key
// that would collide falls through to "_block_2_2" (and so on). Two blocks
// sharing one real id keep sharing it; Contract's own seen-dedupe keeps the
// first, as before.
func SubmitKeys(ids []string) []string {
	keys := make([]string, len(ids))
	taken := make(map[string]bool, len(ids))
	for i := range ids {
		k := strings.TrimSpace(ids[i])
		if k == "" {
			continue
		}
		keys[i] = k
		taken[k] = true
	}
	for i := range keys {
		if keys[i] != "" {
			continue
		}
		base := "_block_" + strconv.Itoa(i)
		k := base
		for n := 2; taken[k]; n++ {
			k = base + "_" + strconv.Itoa(n)
		}
		taken[k] = true
		keys[i] = k
	}
	return keys
}

// Contract derives the input-field contract from a form's builder blocks
// (issue #493). The merchant configures extra menu-checkout fields in the
// normal form builder; the checkout renders exactly those blocks and the
// server validates them with ValidateFields. A form that carries an explicit
// registered_fields contract (issue #254) wins — that contract is the
// merchant/agent's declared shape. Returns nil when the form has neither.
//
// The block's submit key is the field id, matching how the block renderers
// submit ({fields: {block_id: value}}), so a form's own submit path and the
// menu checkout record the same keys. That key comes from SubmitKeys, which is
// also what both public renderers key their controls by — including for a
// legacy block that carries no id (card t_e854b64c), whose synthesized
// "_block_<index>" key is therefore enforced instead of silently skipped.
func Contract(config map[string]any) []Field {
	if regs := FromConfig(config); len(regs) > 0 {
		return regs
	}
	bs := blocksOf(config)
	if len(bs) == 0 {
		return nil
	}
	rawIDs := make([]string, len(bs))
	for i := range bs {
		rawIDs[i] = bs[i].ID
	}
	keys := SubmitKeys(rawIDs)
	var out []Field
	seen := map[string]bool{}
	for i, b := range bs {
		id := keys[i]
		if !collectableTypes[b.Type] || seen[id] {
			continue
		}
		seen[id] = true
		label := strings.TrimSpace(b.Value)
		if label == "" {
			label = id
		}
		f := Field{
			FieldID:      id,
			Type:         b.Type,
			Label:        label,
			Required:     b.Required,
			Placeholder:  b.Placeholder,
			DefaultValue: b.DefaultValue,
			Condition:    b.Condition,
		}
		for _, c := range b.Choices {
			f.Choices = append(f.Choices, Choice{Label: c.Label, Value: c.Value})
		}
		out = append(out, f)
	}
	return out
}

// The DERIVED buyer-identity keys. A public page does not only submit
// {block_id: answer}: it also submits a client{full_name,email,phone} object
// read out of the merchant's name/email/phone BLOCKS, and the submit routes
// re-add that identity to the stored answers under these keys (so the merchant
// sees who responded even on a form with no name block). They are therefore
// answers that carry a block's rule with them — see IdentityBlocks.
const (
	IdentityFullName = "full_name"
	IdentityEmail    = "email"
	IdentityPhone    = "phone"
)

// IdentityBlocks maps each derived buyer-identity key (full_name / email /
// phone) to the id of the contract block that supplies it: the FIRST block of
// the matching type, which is the one both public pages' own mapping reads
// (they query the document for the first name/email/phone control, and the
// contract is in block order). A key with no block of that type is ABSENT —
// its value then came from somewhere else (a legacy page's hand-written input,
// or nothing) and no builder rule of ours applies to it.
//
// The caller needs the pairing because VisibleAnswers works in block ids: it
// prunes a hidden block's own key, and a route that then re-adds the identity
// from client{} would put that same hidden answer back under a derived key —
// the answer the merchant reads as who submitted (card t_5eb85ec8: a hidden
// conditional email block was stored as data["email"] while its own block id
// was correctly absent).
func IdentityBlocks(regs []Field) map[string]string {
	var out map[string]string
	for i := range regs {
		var key string
		switch regs[i].Type {
		case "name":
			key = IdentityFullName
		case "email":
			key = IdentityEmail
		case "phone":
			key = IdentityPhone
		default:
			continue
		}
		if out == nil {
			out = make(map[string]string, 3)
		}
		if _, claimed := out[key]; !claimed {
			out[key] = regs[i].FieldID
		}
	}
	return out
}

// VisibleAnswers returns the answers to RECORD for a submission: the submitted
// answers minus every field whose conditional-logic rule is false for them.
// The second result is the set of hidden field ids, for a caller that has to
// prune a differently-typed map (the response route carries map[string]any).
//
// The contract is the right unit: the builder offers the conditional-logic panel
// for exactly the collectable types (fieldTypes in web/src/app/builder/page.tsx
// — name, email, phone, textarea, number, select, radio, checkbox, date, time,
// website, address, consent, hidden, list, repeater, image-choice, captcha), and
// those are the contract's types. A hand-written config that attached a rule to
// some other block type would have that rule honoured on the page and ignored
// here — the same asymmetry a rule on a block the renderer drops already has.
//
// Why the server drops the answers at all: the public renderers hide a
// conditional block whose rule is false and disable its controls, so the buyer
// never sees it and its answer never leaves the page. A client is not
// authoritative — a direct POST can name any field — so the same rule is applied
// here to what is stored (and to what the confirmation, notification and
// CHIP-feed logic downstream read). Conditions are evaluated against the
// SUBMITTED answers in one pass, so a field that is itself hidden contributes no
// answer and cannot keep a dependent block visible.
func VisibleAnswers(regs []Field, answers map[string]string) (map[string]string, map[string]bool) {
	visible := make(map[string]string, len(answers))
	for k, v := range answers {
		visible[k] = v
	}
	nums := intAnswers(answers)
	var hidden map[string]bool
	for i := range regs {
		r := &regs[i]
		if r.Condition == nil || r.Condition.Met(answers, nums) {
			continue
		}
		if hidden == nil {
			hidden = make(map[string]bool, 1)
		}
		hidden[r.FieldID] = true
		delete(visible, r.FieldID)
	}
	return visible, hidden
}
