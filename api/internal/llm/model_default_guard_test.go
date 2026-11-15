// The guard for the SHIPPED default model (cards t_85e6f366, t_6784f5bb).
//
// WHY THIS FILE EXISTS.
//
// `DefaultModel` is not a decoration — it is what every AI surface falls back to
// when LLM_MODEL is unset (llm.New replaces an empty model with it). It named a
// model the provider RETIRED on 2026-09-25, which answers HTTP 410 Gone, so a
// deployment built without the env override offered the assistant, the builder
// AI draft and the legal drafter and failed on every call. Prod only survived
// because the host carried an override; the trap was in the repo, one unset
// variable away from recurring on a fresh host, a rollback or a new environment.
//
// So the constant is pinned here in three directions:
//
//  1. it is the model that works (not a retired family id);
//  2. a client built with an EMPTY model name resolves to it — the fallback
//     path, which is the half a constant-only assertion cannot see;
//  3. every user-facing string that names a model names the SAME one: the MCP
//     tool descriptions, their entries on the docs page and the builder text.
//     AGENTS.md docs-parity says those move together, so they are asserted
//     together rather than eyeballed.
//
// WHAT "REPO-WIDE" MEANS HERE, EXACTLY.
//
// The previous cut of this header claimed the scan covered "every model id
// written anywhere on the tree". That was false in two ways, both reproduced on
// card t_6784f5bb: the walk read a fixed EXTENSION whitelist, so a retired id in
// a `.mjs`, a `.txt`, a `.py` or the extensionless `Dockerfile` was invisible;
// and the token rule only matched ids beginning `deepseek-`, so an id from any
// other family was never tokenised at all. Both are fixed by INVERTING the
// default, and the exact scope is now stated instead of asserted:
//
//   - the walk starts at the repo root and skips the trees that are not shipped
//     source: `.git`, `node_modules`, `.next`, `vendor`, `test-results` and
//     `.worktrees` (a full checkout per card). `.next` matters for a reason
//     recorded on the card: after a `npm run build`, Turbopack caches the build
//     shell's own environment (ANTHROPIC_MODEL, BRANCH) into `.next/cache`, which
//     is a harness artefact rather than a shipped id;
//   - a SYMLINK is never descended into (filepath.Walk lstat's every entry, so a
//     directory reached through a link is not a directory to it). A link NAMED for
//     a skipped tree — a dev worktree's hand-linked `web/node_modules` is the
//     measured case (card t_f7430361) — is skipped by that same name rule; any
//     other link to a directory, and a broken link, is REFUSED BY NAME with its
//     target, because reading it returned EISDIR and aborted the whole guard while
//     dropping it would narrow the scan. A link to a regular FILE is read through,
//     as it always was;
//   - EVERY remaining file is READ. There is no extension whitelist, so a shape
//     nobody thought of (`.mjs`, `.txt`, `.py`, `.mod`, `.sum`, an extensionless
//     Dockerfile, a file with no extension at all) is read by default. The files
//     not read are `*_test.go` — a fixture must not be able to satisfy a guard
//     about shipped text — the explicit BINARY skip list (skipReadExts: images,
//     fonts, archives, object code), and a file whose FIRST 4 KiB ARE NOT TEXT;
//   - "not text" is decided by CONTENT, not by name (card t_7a1a902c). The walk
//     reads the first maxTextProbe bytes; a NUL byte or invalid UTF-8 there means
//     the file cannot be judged as source, so it is SKIPPED — and every skipped
//     path is NAMED in scope(), so the skip is visible in each red rather than
//     silent. WHY CONTENT AND NOT A PATH: the measured case is `make api`, which
//     writes api/bin/jomform-api into a GITIGNORED tree (.gitignore:2). A fresh
//     worktree never has it, which is why the full sweep on main is green, and a
//     machine that built before gating always has it: the scan used to DIE on it,
//     naming a file the guard was never meant to judge (reproduced on this tree:
//     both repo-wide tests below failed on
//     `the scan could not decode api/bin/jomform-api as text`). A path rule
//     cannot cover the shape — `go build ./cmd/jomform-api` drops the same binary
//     in api/ — and a NAME rule (`bin`) would skip a `bin/` of shipped scripts;
//   - reading a compiled binary as text is not merely useless, it is WRONG. Go
//     folds constant expressions, so this guard's own retiredModelFamily is a
//     LITERAL in the image (measured on the 79.6 MB binary `make api` wrote in
//     this worktree: the retired family 1 occurrence, the shipped id 3). A scan
//     that could decode an image would report the guard's own constant as a stray
//     mention of the retired id;
//   - a file whose first 4 KiB DECODE but which is not valid UTF-8 past that is
//     still REPORTED BY NAME and fails the guard. That shape is a text file with a
//     corrupt region, not build output, and holding that line is what stops the
//     content rule from becoming a silent way to lose a file the guard should have
//     read.
//
// The residual is therefore two sentences long: a model id is found only if it is
// written as plain text in a file that is neither a `*_test.go` file, nor on the
// binary skip list, nor non-text in its first 4 KiB — those skips are named in
// scope(); and a compiled binary whose 4 KiB header happens to decode as text
// (NUL-free, valid UTF-8) would be read and judged like source. A dead id baked
// into an image, or held inside a compressed blob, is out of reach. Measured while
// writing the earlier cut of this header: 701 files read, 395 skipped by the two
// by-name rules, none of them undecodable; on a built tree the content rule's skip
// list is exactly the binary under api/bin, and nothing else.
//
// A MODEL ID IS NOT ONLY A `deepseek-` ID.
//
// modelToken matches the id's FAMILY, and modelFamilies names the families this
// guard judges. The provider list is the trap this closes: it serves other
// families too (measured from this host with the prod deployment's own key on
// 2026-09-28, card t_1f50b935: `gpt-oss:120b` → HTTP 200, `deepseek-v4.1-flash:1017`
// → HTTP 200, while `qwen3-coder:480b` and `deepseek-v3.1:671b` → HTTP 410), so
// a mention of a dead id from another family is exactly the same defect. A
// family that is not on the list cannot be judged, so an id-shaped token
// carrying a model SIZE TAG (`:480b`, `:120b`) is REFUSED BY NAME even when its
// family is unknown — the one shape where silence would be worse than a noisy
// red. Measured over this tree: that rule fires zero times, so it is a net for
// the future rather than a licence to rewrite prose.
//
// WHY THE RETIRED ID IS ASSEMBLED FROM PARTS.
//
// `retiredModelFamily` is built at runtime on purpose: the retired id must not
// be greppable as a model id anywhere on the tree, this file included, because a
// copy-pasteable dead id is how it gets copied back. modelMentionExempt pins the
// one applied migration where it survives as a historical record, and a
// repo-wide scan for the retired family prefix over the shipped tree is the
// check that says so. Reassembling this constant into one literal would put the
// dead id back on the tree under the guard's own name.
//
// EVERY OTHER MENTION IS EITHER THE SHIPPED ID OR AN EXEMPTION WITH A REASON.
// modelMentionExempt is token-scoped and checked for rot, and it refuses to
// shelter the retired family anywhere but that one migration — so "add it to the
// exemption table" is never a way to make a retired id green without saying why.
//
// AN ENGLISH HYPHENATED COMPOUND IS NOT A MODEL ID (card t_dba05702).
//
// Several families in modelFamilies are also ordinary English words — `command`,
// `solar`, `granite`, `falcon`, `phi` — so the `-` branch of modelToken (the
// branch that is load-bearing for `deepseek-coder`, `gemini-pro`, `claude-opus`)
// also matches `command-line`. On the pre-fix reader that was a FALSE POSITIVE:
// the phrase "the command-line half" in a shell check LABEL parsed as an id in
// the `command` family and redd TestUserFacingStringsNameTheSameModelAsTheConstant
// with a message about the shipped LLM default, which is nowhere near the real
// subject of the text. proseCompounds is the token-scoped fix: a small table of
// English compounds, each with a reason and each checked for rot, consulted at
// the ONE place the tokeniser judges a token (readTokenFiles). It is NOT a shape
// rule, and the reason is in the table's own header: `command-line` and the real
// Cohere id `command-r` have the SAME shape, so no rule that inspects the shape
// can separate them — only knowing which token is which can, which is what the
// table records.
//
// THE BITE PROOF. api/scripts/biteproof_model_id_scan.py sabotages one shape at a
// time and requires the guard in this tree to go RED naming the file, while a BASE
// revision's reader stays GREEN on the same mutation — that second leg is the "the
// old scan missed this" column. It is registered in BITE-HARNESSES.md, and it
// refuses to report a verdict when its two readers are byte-identical (a comparison
// of a guard with itself proves nothing).
//
// CASE 15 IS THE SAME PROOF IN THE OTHER DIRECTION (card t_7a1a902c): the mutation
// there is a file the OLD reader could not even READ, so the legs invert — BRANCH
// stays GREEN because it skips the build output and names it, and BASE goes RED
// because it died on it. The harness also refuses a tree that has been BUILT, for
// the same reason: every BASE leg predates the content rule, so api/bin's own binary
// would red every legacy case for a reason that is not its mutation.
//
// THE BASE IS DERIVED, NOT `origin/main`. It used to be `origin/main`, and that was
// correct only until this guard's own rewrite MERGED there: from that merge on,
// `origin/main` IS the widened reader, so every legacy BASE leg compared two
// caught-up readers and read RED while naming no defect (measured: 5 of 10 cases,
// card t_492976fe). The default is now the parent of the newest commit at
// `origin/main` that touched this guard, and it is CAPABILITY-CHECKED — a base whose
// reader already catches the colon shape is refused rather than used.
package llm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// workingModel is the model prod actually runs (LLM_MODEL) and the value this
// repo must default to.
const workingModel = "deepseek-v4.1-flash:1017"

// retiredModelFamily is the retired family PREFIX. Any id starting with it is a
// retired id, tagged or not — the provider's 410 was on the tagged one, and the
// untagged spelling is the same dead family. The prefix does not match
// `deepseek-v4.1-flash:1017` (`.` where the family has `-`), so the working model is
// unaffected by the deny rule.
var retiredModelFamily = "deepseek-v4-" + "flash"

// modelFamilies are the provider families this guard judges. A mention of a
// model id from any of them must name the shipped default, because the default
// is the only id this repo can vouch for: the provider serves some families
// (`gpt-oss:120b` measured 200) and has retired others (`qwen3-coder:480b`,
// `deepseek-v3.1:671b`, `deepseek-v4-flash:0731` all measured 410), so the guard
// does not try to sort live ids from dead ones — it keeps the tree on the one id
// the constant pins, which is what docs-parity already says.
//
// A family is not a fixed shape: it may carry a hyphen (`gpt-oss`, `deepseek`)
// or none at all (`gemma`, `mistral`), and the id's tail is separated from it by
// `-`, a leading digit or a `:` (`gpt-oss:120b`). All three are the SAME claim —
// an id from a family this guard judges — so all three are judged (see
// modelToken and idHasIdSeparator below).
//
// Sorted longest-first so `gpt-oss` is preferred over `gpt` when classifying a
// token; the match in modelFamilyOf is a prefix test, not a substring search.
var modelFamilies = []string{
	"chatglm", "baichuan", "internlm", "moonshot", "nemotron", "starcoder",
	"deepseek", "mixtral", "mistral", "granite", "minimax", "exaone", "gpt-oss",
	"falcon", "gemini", "claude", "llama", "gemma", "qwen", "command", "grok",
	"kimi", "glm", "phi", "gpt", "dbrx", "olmo", "solar",
}

// modelToken matches a model id as it is written in prose, a description or a Go
// string: a FAMILY name (modelFamilies) followed by the id's tail and an
// optional `:tag`. Case-insensitive because prose writes `Qwen3` as readily as
// `qwen3`.
//
// Three properties are deliberate:
//
//   - it must END on a word character, so a trailing sentence period or a
//     closing bracket is not swallowed into the token (`…-flash).`);
//   - the inner `.`/`:` of a real id are kept — a tagged id is a DIFFERENT id
//     and must not be normalised into the untagged one;
//   - the family must be followed by an ID SEPARATOR and nothing else will do:
//     `-` (the shape every hyphenated id uses — `deepseek-v4.1-flash:1017`), a
//     leading DIGIT (the `qwen3` shape), or a `:` with an id-shaped tail (the
//     `id:tag` spelling — `gpt-oss:120b`, `mistral:7b` — which is the only
//     branch that reaches a family carrying no hyphen, because the other two
//     require the very hyphen such a family does not have). Without that the
//     bare family name would become a hit (`phi` and `solar` are English words),
//     and a guard that reports prose is a guard people silence.
//
// WHAT THE SEPARATOR REQUIREMENT IS NOT. It used to be spelled as a blanket
// "the token must contain a digit" check at the call site. The comment justifying
// it named the bare family name, but a digit requirement is a different rule: it
// also discarded real IDS that happen to carry no digit in them — `deepseek-coder`,
// `gemini-pro`, `claude-opus` — and the scan then judged none of them (card
// t_16fa0164). That is a REGRESSION rather than a residual: the pre-widening
// reader matched every `deepseek-…` token and caught `deepseek-coder`, so the
// digit filter lost a shape this guard already had. The rule is now the separator
// itself — `<family><separator>` is an id and is judged, digit or not — and the
// three branches below are exactly the three separator shapes.
//
// WHY THE `:` BRANCH IS NOT JUST `[0-9]`-OR-`-` AGAIN. The two original branches
// both require something a hyphenless family does not have: the first needs the
// `-` itself, the second a leading digit. A family written `id:tag` is therefore
// reached by NEITHER — and one such id is this guard's own measured-live
// exemplar, `gpt-oss:120b` (header above), which `unknownModelTag` cannot catch
// either because that net requires a digit before the colon and `gpt-oss` carries
// none (card t_492976fe). Measured on this tree: a `:` branch fires on zero real
// files, because the only `family:` tokens on the tree are inside this file's own
// comments — and the scan skips `*_test.go`.
var modelToken = regexp.MustCompile(
	`(?i)\b(?:` + strings.Join(modelFamilies, "|") +
		`)(?:-[A-Za-z0-9.:_-]*[A-Za-z0-9]|[0-9](?:[A-Za-z0-9.:_-]*[A-Za-z0-9])?|:(?:[A-Za-z0-9][A-Za-z0-9._-]*[A-Za-z0-9]|[A-Za-z0-9]))`)

// unknownModelTag matches `<name with a digit>:<N>[bm]` — the id:size tag a model
// is written with (`qwen3-coder:480b`, `gpt-oss:120b`, `llama-3:70b`). It is the
// net for a family modelFamilies does not carry: such an id cannot be classified
// (see modelFamilyOf), and silence about it would be the same failure mode the
// extension whitelist had.
//
// The size tag must be digits with an optional lowercase `b`/`m` final, which is
// what keeps it off ports and versions — `localhost:8080`, `valkey:9.1-alpine`,
// `a11y:install` and a Go module path's `h1:…` hash all fail it (measured: zero
// hits on this tree, `api/go.sum` included).
var unknownModelTag = regexp.MustCompile(`\b[a-z][a-z0-9._-]*[0-9][a-z0-9._-]*:[0-9][a-z0-9]*[bm]\b`)

// proseCompounds are tokens modelToken matches that are NOT model ids at all —
// ordinary English hyphenated compounds whose FIRST word happens to be a
// provider family name. A token must never be judged as an id when it is prose:
// the guard would red on a check label, a doc sentence or a code comment about
// something else entirely and name a defect that does not exist.
//
// WHY THIS TABLE EXISTS, MEASURED. Some families in modelFamilies are also
// ordinary English words — `command`, `solar`, `granite`, `falcon`, `phi` are
// the ones that form hyphenated compounds — so the `-` branch of modelToken
// (`<family>-<tail>`, the branch that is load-bearing for `deepseek-coder`,
// `gemini-pro`, `claude-opus`) also matches `command-line`. Card t_dba05702
// measured it on a full local gate (tree df7cf6a8): the phrase "the command-line
// half" in a shell check LABEL parsed as an id in the `command` family and redd
// TestUserFacingStringsNameTheSameModelAsTheConstant with a message about the
// shipped LLM default — nowhere near the real subject of the text. Reproduced
// here on this tree before the fix: putting `command-line` in one file made the
// guard report that file and `command-line` as a stray `command`-family id.
//
// WHERE THE EXCLUSION SITS, AND WHY NOT SOMEWHERE ELSE. It is TOKEN-scoped, like
// modelMentionExempt, so it silences no file and no other id: a real id of the
// SAME family (`command-r7b` — a Cohere id, docs.cohere.com/docs/models) is
// still judged, which is what TestProseCompoundExclusionsAreProseAndDoNotShelterIDs
// pins in both directions. A FILE-scoped exemption would have been the easy
// shape and the wrong one — it would also cover the retired id if somebody wrote
// it into that file later, which is the exact shape this guard exists to catch.
//
// WHY A TABLE AND NOT A SHAPE RULE (the alternative, and why it was refused). A
// bare "the token must carry a digit" rule would also drop `command-line`, but
// it drops real ids too: `deepseek-coder`, `gemini-pro` and `claude-opus` carry
// no digit, and the reader before the widening CAUGHT `deepseek-coder` — so that
// filter is a REGRESSION, not a narrowing of prose, which is why card t_16fa0164
// removed it and why api/scripts/biteproof_model_id_scan.py case 9 and
// TestScanSeesTheShapesTheOldWhitelistCouldNot (fixture
// `nested/other_family_nodigit.go`) both fail if it comes back. `command-line`
// and `gemini-pro` have the SAME shape — `<family>-<word>` — so no shape rule
// can separate them: only knowing that one token is an English compound and the
// other is a published id can. That is what this table records, one token at a
// time, with a reason.
//
// WHAT IT COSTS, STATED PLAINLY. A NEW English compound whose first word is a
// family but whose second word is not listed here (say `solar-powered` in a
// sentence nobody has written yet) still reds the gate the first time it is
// written. That is deliberate, not an oversight: the guard's header records that
// its preference is a NOISY red over a SILENT hole ("the one shape where silence
// would be worse than a noisy red"), and the alternative — treating
// `<family>-<word>` as prose by default — would silently stop judging every
// digit-free id of every such family (`command-r`, `command-light`,
// `solar-pro`, `llama-guard` are all real ids with no digit in them). So the
// entry is added on purpose, with its reason, exactly like a modelMentionExempt
// entry. The check below refuses an entry that has no reason, that the tokeniser
// no longer produces (rot — the exclusion would be guarding a shape that cannot
// occur), that carries a DIGIT (a digit-free compound is prose; a token with a
// digit looks like an id and must be judged, not silenced), that carries a model
// SIZE tag, or that is the shipped/retired id.
var proseCompounds = map[string]string{
	// `command` is both a Cohere family and an ordinary English noun, so
	// `command-<word>` reads as English at least as readily as it reads as an id.
	"command-line":   "ordinary English compound: a shell's command line (the phrase this card was filed on — 'the command-line half' in a check label)",
	"command-prompt": "ordinary English compound: the shell prompt a user types at",
	// `solar` is a family (Upstage) and an English adjective; `solar-<noun>` is
	// ordinary technical prose about the sun.
	"solar-powered": "ordinary English compound: running on solar energy",
	"solar-panel":   "ordinary English compound: a photovoltaic panel",
	"solar-flare":   "ordinary English compound: a burst of solar radiation",
	// `granite` and `falcon` are families and English nouns used as compound
	// modifiers.
	"granite-faced": "ordinary English compound: faced with granite",
	"falcon-eyed":   "ordinary English compound: keen-eyed, after the bird",
	// `phi` is a family and the English name of the Greek letter, whose
	// compounds are technical prose rather than ids.
	"phi-coefficient": "ordinary English compound: a coefficient named for the letter phi",
}

// isProseCompound reports whether a token modelToken matched is a known English
// compound rather than a model id. Case-insensitive for the same reason
// modelToken is: prose writes `Command-Line` as readily as `command-line`.
func isProseCompound(id string) bool {
	_, ok := proseCompounds[strings.ToLower(id)]
	return ok
}

// modelMentionExempt names the mentions that are deliberate, as file -> id ->
// reason. Nothing else is exempt, and the exemption is TOKEN-scoped rather than
// file-scoped on purpose: exempting one id in a file must not also silence a
// retired id somebody adds to that same file later, which is the shape this
// guard exists to catch. Every entry is checked to be REACHED (an exemption the
// scan never matches is rot, and reads as permission it does not grant).
//
// The only shape exempt is an APPLIED MIGRATION. Its comments are the historical
// record of what happened at the time, and editing them churns files the
// fresh-DB guard replays. `000029` named the LLM default of its day.
//
// WHAT USED TO BE HERE, AND WHY IT IS GONE. Cards before t_0e9dc7fc pinned the
// spam judge's model name in eight shipped files here — the judge's package doc,
// its stored spam_reason (which a merchant sees on /spam), the wiring comment in
// main.go, two doc comments, an MCP json-schema description and the /spam page's
// header comment. The table was the record that the judge did NOT run that
// model: every spam_judge job went through the shared
// `llm.New(cfg.LLMBaseURL, cfg.LLMModel)` client. t_0e9dc7fc moved the judge onto
// its own DECISION model and corrected every sentence, so those entries were
// dropped with the mentions rather than left guarding text that no longer
// exists.
//
// THE DECISION MODEL IS A DELIBERATE EXCEPTION TO THIS GUARD'S RULE, not a hole
// in it. The spam judge asks `clef` — an id `GET /v1/models` publishes with
// `"api":"systemone"` — on a different route (/v1/systemone) from the chat model,
// and the chat route answers HTTP 400 for it. Requiring every mention of it to
// spell the CHAT default would make the tree say something false, so `clef` is
// deliberately NOT added to modelFamilies: that list judges ids the repo must
// spell as the chat default, and a decision family in it would red every correct
// mention. Its value is pinned by its own guard instead —
// TestSpamJudgeModelIsThePublishedDecisionModel, plus the opt-in
// TestSpamJudgeModelIsPublishedByTheRouter, both in spam_judge_test.go. A
// router-side rename therefore fails there instead of silently 400-ing every
// submission here.
//
// A retired id may NEVER be parked in this table except for the pinned
// migration: the assertion below refuses an exemption whose id is in the retired
// family, so the one exemption that names it is the historical record and nothing
// can ride in behind it.
var modelMentionExempt = map[string]map[string]string{
	// The retired family, named once, as a historical record.
	"api/internal/db/migrations/000029_space_legal_pages.up.sql": {
		"deepseek-v4-flash": "applied migration — the default of its day, historical record, never edited",
	},
}

// retiredMentionExemptPath is the one path allowed to hold the retired id, kept
// apart from the table above so the assertion that refuses a retired id in every
// OTHER exemption has something to compare against.
const retiredMentionExemptPath = "api/internal/db/migrations/000029_space_legal_pages.up.sql"

// userFacingModelSites are the files that must NAME the shipped model to a user
// or an agent. They are listed because the parity claim is about these three
// surfaces specifically (the MCP tool description, its docs entry and the
// builder copy). Removing the mention from one of them is a deliberate decision,
// not an accident: it must edit this guard, which is the point.
//
// WHAT THIS LIST IS NOT — and this is the sentence that was missing. It claims
// these files MENTION the model. It does NOT claim that
// api/internal/mcpsrv/tools.go *is* the tool registry. Those are two different
// statements and only the first one is true: #732 moved a tool literal
// (`disable_2fa`) out of tools.go into parity_issue396.go, so the registry is
// the PACKAGE (see mcpRegistryDir) and tools.go is one file inside it. Reading
// the registry out of this list was this guard's defect; the registry scope now
// lives in mcpToolFiles, so the entry above stays a mention claim and nothing
// here has to change when a literal moves.
var userFacingModelSites = []string{
	"api/internal/mcpsrv/tools.go",
	"web/src/lib/mcpToolGroups.ts",
	"web/src/app/builder/page.tsx",
}

// docsMCPPageRelPaths is every page of /docs/mcp after the split (card
// t_0356ed4c): the hub plus its four task pages. docsMCPToolGroupsRelPath is the
// module those pages render the tool rows from.
//
// WHY THE DOCS ENTRY ABOVE IS THE MODULE, NOT THE HUB (card t_ee4114af). /docs/mcp
// used to be ONE page, so the hub carried the tool rows AND the model mention and
// naming it was correct. The split moved the rows into the module and left the hub
// as a ~300-word index — no row, no model mention. This guard then failed on a tree
// where every user-facing surface still named the model, because it was reading a
// page that no longer holds the copy. The union below is what "the docs entry" means
// now: any of the pages, or the module they render.
var docsMCPPageRelPaths = []string{
	"web/src/app/docs/mcp/page.tsx",
	"web/src/app/docs/mcp/connect/page.tsx",
	"web/src/app/docs/mcp/tools/page.tsx",
	"web/src/app/docs/mcp/oauth/page.tsx",
	"web/src/app/docs/mcp/troubleshooting/page.tsx",
}

// docsMCPToolGroupsRelPath is the module the /docs/mcp pages import for the tool
// rows. A row's `desc` here is the sentence a reader sees on /docs/mcp/tools.
const docsMCPToolGroupsRelPath = "web/src/lib/mcpToolGroups.ts"

// mcpRegistryDir is the MCP tool registry, in the scope the rest of the repo
// already agrees on: the PACKAGE, not one FILE.
//
// WHY IT IS A DIRECTORY. A `&mcp.Tool{}` literal can live in any file of this
// package — #732 moved `disable_2fa` into parity_issue396.go — so a reader that
// names tools.go answers about a subset of the registry. The two web guards
// (web/src/lib/mcpToolInventory.test.ts, web/src/lib/docsToolClaims.test.ts) and
// the docs page body were widened to `api/internal/mcpsrv/*.go` for exactly that
// reason; this file was not, which is the defect this constant closes. Concretely
// it made the docs-parity check below able to compare the page against a
// description left behind in tools.go, and made a description that merely MOVED
// read as a fatal about tools.go.
const mcpRegistryDir = "api/internal/mcpsrv"

// TestDefaultModelIsNotRetired pins the constant itself. Two assertions, because
// they fail differently: the exact id says WHICH model ships, and the family
// deny rule still bites if someone swaps in another id from the retired family
// (a tag change is not a fix — the whole family is gone).
func TestDefaultModelIsNotRetired(t *testing.T) {
	if DefaultModel == "" {
		t.Fatal("DefaultModel is empty: every AI surface would call the provider with no model at all, " +
			"which is the outage this constant is supposed to prevent")
	}
	if strings.HasPrefix(DefaultModel, retiredModelFamily) {
		t.Fatalf("DefaultModel = %q, which is the RETIRED family %q — the provider answers HTTP 410 Gone for it, "+
			"so a deployment without the LLM_MODEL override dies on every AI call while still offering the "+
			"feature in the UI. That is the outage this guard exists for.", DefaultModel, retiredModelFamily)
	}
	if DefaultModel != workingModel {
		t.Fatalf("DefaultModel = %q, want %q (the model prod runs via LLM_MODEL and the one the provider serves). "+
			"A different id must be a deliberate change to this guard and to the user-facing strings together.",
			DefaultModel, workingModel)
	}
}

// TestEmptyModelResolvesToTheWorkingDefault pins the FALLBACK, which is the half
// a constant assertion cannot see: llm.New is what every caller passes its
// config through, and an empty LLM_MODEL is exactly the state that took prod
// down. An empty model must resolve to the working default — not to the retired
// id, not to "", and not to whatever a future edit to New happens to do.
//
// The override half is asserted in the same test on purpose: a "fix" that made
// New ignore the caller's model would pass the first assertion and break every
// deployment that sets LLM_MODEL deliberately.
func TestEmptyModelResolvesToTheWorkingDefault(t *testing.T) {
	t.Run("an empty model uses the shipped default", func(t *testing.T) {
		c := New("", "")
		if c == nil {
			t.Fatal("New(\"\", \"\") returned nil — there is no client to call the provider with")
		}
		if c.model == "" {
			t.Fatal("a client built with an empty model carries an empty model: the request has no model field, " +
				"so every AI surface fails even though the default exists")
		}
		if strings.HasPrefix(c.model, retiredModelFamily) {
			t.Fatalf("a client built with an empty model resolved to %q — a retired model. This is the "+
				"unset-LLM_MODEL outage reproduced exactly: the deployment looks healthy and every call is a 410.",
				c.model)
		}
		if c.model != workingModel {
			t.Fatalf("a client built with an empty model resolved to %q, want %q", c.model, workingModel)
		}
		if c.model != DefaultModel {
			t.Fatalf("the resolved model %q is not DefaultModel %q — the constant has stopped being the fallback, "+
				"so pinning the constant no longer says anything about what ships", c.model, DefaultModel)
		}
	})

	t.Run("an explicit model still wins", func(t *testing.T) {
		c := New("", "some-other-model")
		if c.model != "some-other-model" {
			t.Fatalf("New(baseURL, %q) resolved to %q — the caller's model must be honoured; a default that "+
				"overrides an explicit choice would silently ignore LLM_MODEL in every deployment",
				"some-other-model", c.model)
		}
	})
}

// TestModelMentionExemptionsAreRealAndDoNotShelterTheRetiredId keeps the
// exemption table honest in both directions: an entry the scan never reaches is
// rot (it reads as permission that does not exist), and an entry covering the
// retired family anywhere but the pinned migration would let the outage back in
// by paperwork. The check runs against the real tree, so it cannot drift from
// the scan it excuses.
func TestModelMentionExemptionsAreRealAndDoNotShelterTheRetiredId(t *testing.T) {
	root := repoRoot(t)
	hits := readTokenFiles(t, root).hits

	if len(modelMentionExempt) == 0 {
		t.Fatal("modelMentionExempt is empty — if every mention was really fixed, delete this table and this " +
			"test with it; an empty table would otherwise read as a decision it is not")
	}
	for rel, ids := range modelMentionExempt {
		found, ok := hits[rel]
		if !ok {
			t.Errorf("modelMentionExempt names %s, which the scan found no model id in — the file was "+
				"rewritten or renamed, and an exemption for a file that writes no id excuses nothing", rel)
			continue
		}
		for id, why := range ids {
			if why == "" {
				t.Errorf("modelMentionExempt[%s][%s] has an empty reason — an exemption without a reason is "+
					"indistinguishable from an oversight", rel, id)
			}
			if _, ok := found[id]; !ok {
				t.Errorf("modelMentionExempt exempts %q in %s, but the scan no longer finds that id there — "+
					"the mention was fixed, so drop the exemption with it", id, rel)
			}
			if strings.HasPrefix(id, retiredModelFamily) && rel != retiredMentionExemptPath {
				t.Errorf("modelMentionExempt exempts the RETIRED id %q in %s. Nothing may shelter that family "+
					"except the pinned historical migration (%s) — the provider answers HTTP 410 for it and the "+
					"mention belongs in the diff, not in this table.", id, rel, retiredMentionExemptPath)
			}
		}
	}
}

// TestUserFacingStringsNameTheSameModelAsTheConstant asserts acceptance 3: the
// MCP tool descriptions, their docs entries and the builder copy name the model
// the code actually defaults to — checked as text, not by eye.
//
// It walks the whole repo (test files excluded, so no fixture can satisfy it)
// and requires every model id written anywhere to be the working one, with the
// documented exemptions. Direction matters both ways:
//
//   - revert the constant  -> every user-facing string now disagrees with it;
//   - revert a description -> that file names the retired id;
//   - add a NEW file naming a retired model -> it appears in the report by name,
//     whatever its extension.
//
// The failure text carries a REASON per hit, not a bare id, because the shapes
// are not the same defect: a mention of the retired family is the outage, while a
// mention of another family's id is a decision somebody has to make on purpose.
// Both are red, and the message says which one it is.
func TestUserFacingStringsNameTheSameModelAsTheConstant(t *testing.T) {
	root := repoRoot(t)
	scan := scanRepoForModelIDs(t, root)
	hits := scan.hits

	// The anchor: the constant's own definition must be among the hits, or this
	// guard is reading the wrong tree and would pass vacuously.
	if why, ok := hits["api/internal/llm/llm.go"][workingModel]; !ok || why != "" {
		t.Fatalf("api/internal/llm/llm.go does not spell the working model %q as the shipped default — the scan "+
			"is anchored on the wrong file and proves nothing", workingModel)
	}

	// Every hit, minus the documented exemptions, must be the working model.
	var stray []string
	for rel, ids := range hits {
		exempt := modelMentionExempt[rel]
		for id, why := range ids {
			if why == "" {
				continue // the shipped default
			}
			if _, ok := exempt[id]; ok {
				continue
			}
			stray = append(stray, rel+": "+id+" — "+why)
		}
	}
	sort.Strings(stray)
	if len(stray) > 0 {
		t.Fatalf("these files name a model id that is NOT the shipped default %q:\n  %s\n\n%s\n\n"+
			"Point each mention at %q, or — if the mention is legitimately deliberate (a historical "+
			"migration, or prose about a surface that has not moved yet) — add that file's id to "+
			"modelMentionExempt with its reason. A retired id cannot be parked there except for the pinned "+
			"migration; this guard refuses it.",
			workingModel, strings.Join(stray, "\n  "), scan.scope(), workingModel)
	}

	// The user-facing surfaces must NAME it (a file that stops mentioning a
	// model cannot be checked for disagreement, and docs-parity says these
	// entries move with the tool).
	for _, rel := range userFacingModelSites {
		if why, ok := hits[rel][workingModel]; !ok || why != "" {
			t.Fatalf("%s does not name the model %q. These surfaces tell a user or an agent what the feature "+
				"runs on, and AGENTS.md docs-parity keeps the MCP tool description and its docs entry in step — "+
				"if the mention is being removed deliberately, remove it from this guard in the same change.",
				rel, workingModel)
		}
	}

	// AGENTS.md docs-parity: the MCP tool description and its docs entry are the
	// same sentence. Escapes differ (`\u2014` on the page, the character in Go),
	// so the comparison normalises them first — otherwise the check would be
	// satisfied by two identical strings and fail on two identical strings.
	//
	// The docs entry is looked up across the WHOLE /docs/mcp surface (card
	// t_ee4114af): after the split the rows live in the module the pages render,
	// so a failure must name the file the entry is actually in rather than a hub
	// page that holds no rows.
	toolsDesc := toolDescriptions(t, hits, root)
	docsDesc := docsPageDescriptions(t, root)
	for _, name := range []string{"generate_form", "generate_legal_page"} {
		goDesc, okGo := toolsDesc[name]
		entry, okPage := docsDesc[name]
		if !okGo {
			t.Fatalf("%s holds no %q tool description — the docs-parity half cannot be checked and "+
				"would pass vacuously. If the tool was renamed, rename it on the /docs/mcp surface "+
				"(looked in: %s) and in this guard together; if the literal moved, this reader "+
				"already follows it across the package, so the name is gone from the registry "+
				"rather than from one file.",
				mcpRegistryDir, name, strings.Join(docsSurfaceFiles(), ", "))
		}
		if !okPage {
			t.Fatalf("no /docs/mcp surface file has a %q entry — looked in: %s. The MCP tool "+
				"description and its docs entry must move together (AGENTS.md docs-parity).",
				name, strings.Join(docsSurfaceFiles(), ", "))
		}
		if goDesc != entry.desc {
			t.Fatalf("the %q description differs between the registry (%s) and the docs entry "+
				"(%s):\n  %s: %q\n  %s: %q\n"+
				"They are the same sentence on two surfaces and must move together.",
				name, mcpRegistryDir, entry.file, mcpRegistryDir, goDesc, entry.file, entry.desc)
		}
	}
}

// TestScanSeesTheShapesTheOldWhitelistCouldNot pins the two blind spots card
// t_6784f5bb reproduced, as properties of the SCAN rather than of the tree: a
// tokeniser that cannot see `.mjs`/extensionless files, or cannot see an id from
// another family, is what let a dead id read as green. Both were proven to pass
// on the old code by mutation; here they are required to be seen and refused.
//
// The fixtures are written in a scratch tree (never into the repo — a write here
// would make the guard's own subject a permanent red) and asserted through the
// same readTokenFiles the guard uses, so the two cannot disagree about what the
// scan reads. api/scripts/biteproof_model_id_scan.py is the same claim proved the
// other way round: in the REAL tree, against the pre-fix reader.
func TestScanSeesTheShapesTheOldWhitelistCouldNot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested", "deep"), 0o755); err != nil {
		t.Fatalf("prepare fixture tree: %v", err)
	}
	// The dead id, assembled so this file is not itself a greppable copy.
	dead := retiredModelFamily + ":0731"
	fixtures := map[string]string{
		// (a) an extension the old whitelist did not carry at all
		"nested/dead.mjs": "// default model: " + dead + "\n",
		// (a) an extensionless file — filepath.Ext is "" here
		"nested/Dockerfile": "ARG QA_MODEL=" + dead + "\n",
		// (a) a plain text file a server hands out
		"nested/llms.txt": "model: " + dead + "\n",
		// (b) a dead id from a family that does not begin `deepseek-`
		"nested/other_family.go": "// The LLM (qwen3-coder:480b via the internal endpoint)\n",
		// (c) a LISTED family's id with NO DIGIT ANYWHERE, which is the shape the
		// digit filter dropped (card t_16fa0164): `<family>-tail` is an id, and
		// whether it happens to contain a digit says nothing. Kept beside the
		// other-family fixture because the two are different failures — the one
		// above is a family the old token rule never reached, this one is a shape
		// the pre-widening reader CAUGHT and the widened one lost.
		"nested/other_family_nodigit.go": "// The drafter calls gemini-pro via the internal endpoint\n",
		// (c) a LISTED family's id in the `id:tag` spelling, whose family carries
		// NO HYPHEN — so neither old branch could reach it: the hyphen branch
		// needs the `-` itself and the digit branch a leading digit. This is the
		// shape card t_492976fe opened on, and `unknownModelTag` cannot cover it
		// either because that net needs a digit BEFORE the colon. The exemplar
		// this guard's own header records as measured LIVE (`gpt-oss:120b`, the
		// hyphenated-family variant of the same gap) is pinned by
		// api/scripts/biteproof_model_id_scan.py rather than repeated here.
		"nested/colon_tagged.go": "// The drafter calls mistral:7b via the internal endpoint\n",
		// (b) an id whose family is not even on modelFamilies' list
		"nested/unknown_family.md": "runs on ernie-4.5:8b today\n",
		// the shape that already bit before this card, kept so the widening
		// cannot trade a live proof away
		"nested/control.ts": "export const M = \"" + dead + "\";\n",
	}
	for rel, body := range fixtures {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", rel, err)
		}
	}

	for _, dir := range []string{"node_modules", ".next", ".worktrees"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("prepare skipped dir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "ignored.mjs"), []byte(dead+"\n"), 0o644); err != nil {
			t.Fatalf("write skipped-dir fixture: %v", err)
		}
	}
	// A no-extension file that is not a model id must NOT be reported: the walk
	// reading everything is not the same as the tokeniser reporting everything.
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("test:\n	go test ./...\n"), 0o644); err != nil {
		t.Fatalf("write Makefile fixture: %v", err)
	}
	// The same rule for an English hyphenated compound whose first word is a
	// family name (`command-line`, card t_dba05702): the tokeniser matches it, so
	// it is only the prose-compound exclusion that keeps it out of the report.
	// It is a NEGATIVE fixture, so it must not appear in `hits` below.
	if err := os.WriteFile(filepath.Join(root, "nested/prose_compound.md"),
		[]byte("// the command-line half; a solar-powered sensor\n"), 0o644); err != nil {
		t.Fatalf("write prose-compound fixture: %v", err)
	}

	scan := readTokenFiles(t, root)
	hits, read, skipped := scan.hits, scan.filesRead, scan.filesSkipped
	if read != len(fixtures)+2 {
		t.Fatalf("the scan read %d files under the fixture tree, want %d (the fixtures, the control "+
			"Makefile and the negative control prose_compound.md) — the walk is reading a different set "+
			"than the files it is supposed to judge (skipped %d)", read, len(fixtures)+2, skipped)
	}
	if skipped != 0 {
		t.Errorf("the scan skipped %d files, want 0: nothing in the fixture tree is a *_test.go file, and no "+
			"file carries an extension on the binary skip list. A file inside a skipped TREE is never walked "+
			"at all, so it must not be counted here either — a non-zero number means the skip rules are "+
			"rejecting a file they have no reason to", skipped)
	}

	want := []string{
		"nested/Dockerfile",
		"nested/control.ts",
		"nested/dead.mjs",
		"nested/llms.txt",
		"nested/other_family.go",
		"nested/other_family_nodigit.go",
		"nested/colon_tagged.go",
		"nested/unknown_family.md",
	}
	for _, rel := range want {
		if _, ok := hits[rel]; !ok {
			t.Errorf("%s writes a model id that is not the shipped default and the scan did not report it — "+
				"this is the blind spot card t_6784f5bb reproduced: %s", rel, blindSpotNote(rel))
		}
	}
	if len(hits) != len(want) {
		var got []string
		for rel := range hits {
			got = append(got, rel)
		}
		sort.Strings(got)
		t.Errorf("the scan reported %d files, want exactly the %d whose id is not the shipped default: %v — "+
			"a hit no fixture wrote means the token rule fires on ordinary prose", len(hits), len(want), got)
	}

	// Every judged fixture must carry a REASON, so a red names the defect rather
	// than only the id: a scan that sees a dead id but has nothing to say about
	// it would still let a reader shrug.
	for rel, ids := range hits {
		for id, why := range ids {
			if id == workingModel {
				t.Errorf("%s: the scan classified %q as the shipped default — the stray verdict would pass", rel, id)
			}
			if why == "" {
				t.Errorf("%s: %q was judged stray with an empty reason — the failure text would name no defect", rel, id)
			}
			if rel == "nested/unknown_family.md" && !strings.Contains(why, "SIZE tag") {
				t.Errorf("%s: %q was refused with %q, want the unknown-family reason — an unknown family must "+
					"fail by its SIZE tag, not by accident", rel, id, why)
			}
		}
	}
}

// TestScanSkipsNonTextInputsItCannotJudge pins the THIRD way this guard died on a
// tree it should have judged (card t_7a1a902c): a COMPILED BINARY under
// api/bin/jomform-api, the 80 MB ELF `make api` writes there.
//
// api/bin is GITIGNORED (.gitignore:2), so the file never appears in git and a
// fresh worktree never has it — which is exactly why the weekly full sweep on main
// is green and why this was invisible until someone gated a checkout they had
// already built on. On such a tree BOTH repo-wide tests above failed on one line:
//
//	the scan could not decode api/bin/jomform-api as text, so it cannot judge the file
//
// That is a red naming a file the guard was never meant to judge, and it is not
// even the right answer if the scan COULD decode it: Go folds constant
// expressions, so this guard's own retiredModelFamily is a literal inside the
// image (measured on that binary: 1 occurrence of the retired family, 3 of the
// shipped id). Reading build output as text would report the guard's own constant
// as a stray mention of the retired id.
//
// The fixtures are a scratch tree, never the repo: a binary written into this
// checkout would make the guard's own subject a permanent red — and it would be
// gitignored, so nothing would tell the next reader why the suite was slow.
//
// The four legs, each a different half of the rule:
//
//  1. the measured shape — a small ELF at api/bin/jomform-api, in a tree with a
//     text file BEFORE it and one AFTER it. The walk must finish, the binary must
//     be SKIPPED AND NAMED, and both siblings must still be judged: `return
//     filepath.SkipDir` here would drop the rest of the directory silently, which
//     is the narrowing this guard exists to remove;
//  2. an image whose 4 KiB header is ASCII — only the NUL bytes give it away. This
//     leg is why the rule is "NUL or invalid UTF-8" and not "invalid UTF-8": an
//     ELF's header is largely ASCII, and a binary that decoded would be judged;
//  3. a text file that stops decoding MID-file is still REFUSED BY NAME. That
//     shape is a corrupt region in something that is otherwise text rather than
//     build output, and holding that line is what stops the content rule from
//     becoming a silent way to drop a file the guard should have read;
//  4. a text file whose 4096-byte boundary CUTS A RUNE is read, not skipped: the
//     probe's last byte is a valid start byte with no room for its continuation,
//     and calling that "not text" would silently lose a real source file.
func TestScanSkipsNonTextInputsItCannotJudge(t *testing.T) {
	dead := retiredModelFamily + ":0731"

	t.Run("a compiled binary under api/bin is skipped, named, and does not stop the walk", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "api", "bin"), 0o755); err != nil {
			t.Fatalf("prepare the build-output tree: %v", err)
		}
		// A small ELF: e_ident magic, its NUL padding, a high-byte run, and the
		// retired id as a literal — the same three properties measured on the real
		// binary (invalid UTF-8 at byte 96, 3315 NULs in the first 4 KiB, the
		// retired family folded in once). The id being IN the fixture is what makes
		// "skipped" different from "read and happened to find nothing".
		elf := append([]byte("\x7fELF\x02\x01\x01\x00"), make([]byte, 8)...)
		elf = append(elf, []byte{0xf8, 0xff, 0x80, 0x90}...)
		elf = append(elf, []byte("\x00model "+dead+"\x00")...)
		if err := os.WriteFile(filepath.Join(root, "api", "bin", "jomform-api"), elf, 0o755); err != nil {
			t.Fatalf("write the binary fixture: %v", err)
		}
		before := "api/bin/aa_before.ts"
		after := "api/bin/zz_after.go"
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(before)),
			[]byte("export const M = \""+workingModel+"\";\n"), 0o644); err != nil {
			t.Fatalf("write the file before the binary: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(after)),
			[]byte("// the drafter calls gemini-pro\n"), 0o644); err != nil {
			t.Fatalf("write the file after the binary: %v", err)
		}

		res, err := scanTree(root)
		if err != nil {
			t.Fatalf("the scan refused a tree holding a compiled binary (%v) — this is the measured red: "+
				"`make api` writes api/bin/jomform-api into a GITIGNORED tree, so any machine that builds "+
				"before gating hits it, and the guard dies naming a file it was never meant to judge", err)
		}
		if len(res.nonText) != 1 || res.nonText[0] != "api/bin/jomform-api" {
			t.Fatalf("the scan recorded non-text files %v, want exactly [api/bin/jomform-api] — a skip "+
				"that is not NAMED is the silence the extension whitelist hid behind", res.nonText)
		}
		if res.filesRead != 2 || res.filesSkipped != 1 {
			t.Errorf("the scan read %d files and skipped %d, want 2 read (both text siblings) and 1 "+
				"skipped (the binary) — the skip must cost the binary and nothing else",
				res.filesRead, res.filesSkipped)
		}
		if _, judged := res.hits["api/bin/jomform-api"]; judged {
			t.Errorf("the scan JUDGED api/bin/jomform-api (hits: %v) — a compiled binary is not source, and "+
				"Go folds this guard's own retiredModelFamily into it, so the verdict would be about the "+
				"guard's constant rather than about a shipped mention", res.hits["api/bin/jomform-api"])
		}
		if why, ok := res.hits[before][workingModel]; !ok || why != "" {
			t.Errorf("%s (walked BEFORE the binary) was not judged: %v — the skip stopped the walk early", before, res.hits[before])
		}
		if why, ok := res.hits[after]["gemini-pro"]; !ok || why == "" {
			t.Fatalf("%s (walked AFTER the binary) was not judged as a stray id: %v — a skip that answers a "+
				"file with filepath.SkipDir silently drops the REST of the containing directory", after, res.hits[after])
		}
		if scope := res.scope(); !strings.Contains(scope, "api/bin/jomform-api") {
			t.Fatalf("the scope line does not name the file it skipped: %q. Every red prints this line, so a "+
				"skip it does not name is invisible to the reader of the red", scope)
		}
	})

	t.Run("an image whose header is ASCII is caught by its NUL bytes", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "web", "tools"), 0o755); err != nil {
			t.Fatalf("prepare the fixture tree: %v", err)
		}
		// Valid UTF-8 throughout, and a NUL in it: the shape an "invalid UTF-8"
		// rule alone would read as a text file and judge.
		image := "GCC: (GNU) 13.2.0\x00.symtab\x00model " + dead + "\x00"
		if err := os.WriteFile(filepath.Join(root, "web", "tools", "cli"), []byte(image), 0o755); err != nil {
			t.Fatalf("write the NUL-carrying fixture: %v", err)
		}
		res, err := scanTree(root)
		if err != nil {
			t.Fatalf("the scan refused a NUL-carrying image: %v", err)
		}
		if len(res.nonText) != 1 || res.nonText[0] != "web/tools/cli" {
			t.Fatalf("the scan recorded non-text files %v, want exactly [web/tools/cli] — a NUL byte cannot "+
				"appear in a UTF-8 source file, so this is not text whatever its other bytes look like", res.nonText)
		}
		if len(res.hits) != 0 {
			t.Fatalf("the scan judged a NUL-carrying image: %v", res.hits)
		}
	})

	t.Run("a text file that stops decoding mid-file is still refused by name", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
			t.Fatalf("prepare the fixture tree: %v", err)
		}
		// The first maxTextProbe bytes are text, so the content rule does not skip
		// it — and everything after them is not.
		body := bytes.Repeat([]byte("a"), maxTextProbe)
		body = append(body, 0xf8, 0xff)
		if err := os.WriteFile(filepath.Join(root, "api", "notes.md"), body, 0o644); err != nil {
			t.Fatalf("write the corrupt-text fixture: %v", err)
		}
		_, err := scanTree(root)
		var unreadable *unreadableFileError
		if !errors.As(err, &unreadable) {
			t.Fatalf("the scan returned %v, want the by-name refusal for api/notes.md — a file whose 4 KiB "+
				"header decoded and which stops decoding after that is a text file with a corrupt region, "+
				"and skipping it would be the silent narrowing the content rule must not add", err)
		}
		if unreadable.path != "api/notes.md" {
			t.Fatalf("the refusal names %q, want api/notes.md", unreadable.path)
		}
	})

	t.Run("a text file whose probe boundary cuts a rune is read, not skipped", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
			t.Fatalf("prepare the fixture tree: %v", err)
		}
		// 4095 ASCII bytes then a two-byte rune: the probe ends on the rune's FIRST
		// byte, which is invalid on its own.
		body := bytes.Repeat([]byte("a"), maxTextProbe-1)
		body = append(body, []byte("\u00e9 // the drafter calls gemini-pro\n")...)
		if err := os.WriteFile(filepath.Join(root, "api", "cut_rune.txt"), body, 0o644); err != nil {
			t.Fatalf("write the cut-rune fixture: %v", err)
		}
		res, err := scanTree(root)
		if err != nil {
			t.Fatalf("the scan refused a text file whose probe boundary cut a rune: %v", err)
		}
		if len(res.nonText) != 0 {
			t.Fatalf("the scan skipped a TEXT file (%v) because its 4096-byte probe ended mid-rune — that "+
				"would lose a real source file, and the id after the rune is exactly such a mention", res.nonText)
		}
		if why, ok := res.hits["api/cut_rune.txt"]["gemini-pro"]; !ok || why == "" {
			t.Fatalf("the file was not judged (hits: %v) — it decoded in full, so its mention must be reported",
				res.hits)
		}
	})
}

// TestScanTreatsASymlinkedSkippedTreeAsSkipped pins the SECOND way this guard
// died on a tree it should have judged (card t_f7430361): `web/node_modules` as a
// SYMLINK.
//
// filepath.Walk lstats every entry, so a directory reached through a link is not
// a directory to it — the `node_modules` arm of skipDirs never fired, the entry
// fell through to os.ReadFile, and that returns EISDIR:
//
//	walk <tree> for model ids: read <tree>/web/node_modules: is a directory
//
// which aborts both repo-wide guards within seconds of starting, with a message that reads like a
// broken guard and names no product defect. It is not academic: a dev worktree
// symlinks its node_modules by hand to skip an ~81s `npm ci` (the same shortcut
// whose other symptom is Turbopack panicking at `next dev`), and a hand-linked
// tree is not a seeded one, so deploy/web_modules.sh refusing a symlinked donor
// (card t_eaf5edd4) never sees it.
//
// The fixture is a scratch tree, never the repo: a symlink written into the repo
// would make the guard's own subject a permanent red. It is deliberately SMALL and
// the link's TARGET is a second temp dir holding a retired id, so the test proves
// three things at once:
//
//  1. the walk finishes (on the pre-fix bytes this call itself Fatals with the
//     EISDIR line above — that is the red this test exists to remove);
//  2. the tree behind the link is NOT judged — its bytes are another tree's, and
//     following the link is the one thing the fix must not do;
//  3. the walk keeps going: a file BEFORE the link and one AFTER it are both
//     read. `filepath.SkipDir` returned for a NON-directory entry skips the rest
//     of the containing directory, so answering the link with SkipDir would drop
//     `web/zz_after_the_link.ts` while still passing (1) and (2) — the sibling is
//     what catches that.
func TestScanTreatsASymlinkedSkippedTreeAsSkipped(t *testing.T) {
	root := t.TempDir()
	// The far side of the link: a node_modules-shaped tree with a retired id in
	// it, i.e. exactly the id the scan WOULD report if it followed the link.
	outside := t.TempDir()
	dead := retiredModelFamily + ":0731"
	if err := os.MkdirAll(filepath.Join(outside, "left-pad"), 0o755); err != nil {
		t.Fatalf("prepare the linked tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "left-pad", "index.js"),
		[]byte("module.exports = '"+dead+"';\n"), 0o644); err != nil {
		t.Fatalf("write the linked tree's fixture: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "web"), 0o755); err != nil {
		t.Fatalf("prepare fixture tree: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "web", "node_modules")); err != nil {
		t.Fatalf("link the fixture node_modules: %v", err)
	}
	before := filepath.Join(root, "web", "app.ts")
	after := filepath.Join(root, "web", "zz_after_the_link.ts")
	if err := os.WriteFile(before, []byte("// the default model: "+workingModel+"\n"), 0o644); err != nil {
		t.Fatalf("write the file before the link: %v", err)
	}
	if err := os.WriteFile(after, []byte("// the drafter calls gemini-pro\n"), 0o644); err != nil {
		t.Fatalf("write the file after the link: %v", err)
	}

	scan := readTokenFiles(t, root)
	hits, read, skipped := scan.hits, scan.filesRead, scan.filesSkipped

	if read != 2 {
		var got []string
		for rel := range hits {
			got = append(got, rel)
		}
		sort.Strings(got)
		t.Fatalf("the scan read %d files under a tree with two real files and one symlinked tree, want 2 — "+
			"the link is not shipped source and the files beside it are (skipped %d, hits %v)", read, skipped, got)
	}
	if skipped != 0 {
		t.Errorf("the scan skipped %d files, want 0: the link is a TREE, so it is never walked at all and "+
			"must not be counted as a skipped file", skipped)
	}
	if len(hits) != 2 {
		t.Fatalf("the scan reported %d files, want exactly the 2 beside the link: %v — a third means the "+
			"walk read through the symlink into another tree's bytes", len(hits), hits)
	}
	for rel := range hits {
		if strings.Contains(rel, "node_modules") {
			t.Errorf("the scan judged %s, which lives behind a symlink — its bytes belong to the tree the "+
				"link points at (%s), so the verdict would depend on a tree this checkout does not own",
				rel, outside)
		}
	}
	if why, ok := hits["web/app.ts"][workingModel]; !ok || why != "" {
		t.Errorf("web/app.ts (the file BEFORE the link) was not read, or its %q was not classified as the "+
			"shipped default (got %v) — the walk stopped early", workingModel, hits["web/app.ts"])
	}
	if why, ok := hits["web/zz_after_the_link.ts"]["gemini-pro"]; !ok || why == "" {
		t.Fatalf("web/zz_after_the_link.ts (the file AFTER the link) was not judged as a stray id "+
			"(got %v) — a skip that answers the link with filepath.SkipDir silently drops the REST of the "+
			"containing directory, which narrows the scan exactly like the extension whitelist did", hits["web/zz_after_the_link.ts"])
	}
}

// TestScanRefusesASymlinkedTreeItCannotJudgeByName pins the OTHER half of the
// symlink rule (card t_f7430361): a link the skip list does not name is not read
// (the EISDIR abort) and not silently dropped (which would narrow the scan), it is
// REFUSED by name with its target. Neither case can be a skipped-name tree —
// `node_modules` is what the test above covers — because the whole point here is
// that the walk will not follow a link into another checkout and will not pretend
// it did not see one.
func TestScanRefusesASymlinkedTreeItCannotJudgeByName(t *testing.T) {
	t.Run("a symlinked directory the skip list does not name", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "not_shipped_source")); err != nil {
			t.Fatalf("link the fixture directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("read by the scan\n"), 0o644); err != nil {
			t.Fatalf("write the sibling fixture: %v", err)
		}

		_, err := scanTree(root)
		if err == nil {
			t.Fatal("the scan accepted a tree holding a symlinked directory it does not skip — silently " +
				"not reading it is the narrowing this guard exists to remove, and following it would judge " +
				"another checkout's bytes")
		}
		if strings.Contains(err.Error(), "is a directory") {
			t.Fatalf("the scan still tried to READ the link (%v) — that read is what aborted both repo-wide "+
				"guards with `read <tree>/web/node_modules: is a directory`", err)
		}
		for _, want := range []string{"not_shipped_source", outside, "symlink"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal %q does not name %q — a red has to say WHICH link and where it points, "+
					"or it reads like a broken guard", err, want)
			}
		}
	})

	t.Run("a broken symlink", func(t *testing.T) {
		root := t.TempDir()
		missing := filepath.Join(t.TempDir(), "gone")
		if err := os.Symlink(missing, filepath.Join(root, "vendored")); err != nil {
			t.Fatalf("link the fixture directory: %v", err)
		}

		_, err := scanTree(root)
		if err == nil {
			t.Fatal("the scan accepted a broken symlink — it has no bytes to read, so the verdict would be " +
				"silently narrower than the tree it claims to cover")
		}
		for _, want := range []string{"vendored", missing, "BROKEN"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal %q does not name %q", err, want)
			}
		}
	})

	// A link to a regular FILE is still read through, as it always was: a path
	// that serves readable bytes is judged, a tree the walk declines to enter is
	// not. Pinned so the directory rule cannot be widened into "every link is an
	// error" without saying so.
	t.Run("a symlink to a file is read", func(t *testing.T) {
		root := t.TempDir()
		real := filepath.Join(root, "real_notes.md")
		if err := os.WriteFile(real, []byte("// the drafter calls gemini-pro\n"), 0o644); err != nil {
			t.Fatalf("write the fixture file: %v", err)
		}
		if err := os.Symlink("real_notes.md", filepath.Join(root, "linked_notes.md")); err != nil {
			t.Fatalf("link the fixture file: %v", err)
		}

		res, err := scanTree(root)
		if err != nil {
			t.Fatalf("the scan refused a symlink to a regular file: %v", err)
		}
		hits, read := res.hits, res.filesRead
		if read != 2 {
			t.Errorf("the scan read %d files, want 2 (the real file and the link to it): %v", read, hits)
		}
		if _, ok := hits["linked_notes.md"]["gemini-pro"]; !ok {
			t.Errorf("the linked file's id was not judged (hits: %v) — reading a file's bytes is what the "+
				"scan does at a path; only a TREE is refused", hits)
		}
	})
}

// TestProseCompoundExclusionsAreProseAndDoNotShelterIDs pins the prose-compound
// exclusion (card t_dba05702) in the directions that matter, on a scratch tree
// so no fixture ever lands in the repo:
//
//  1. the table is honest — every entry has a reason, and the token is still
//     PRODUCED by the tokeniser (rot: an entry for a shape modelToken cannot
//     match reads as a decision that excuses nothing);
//  2. an English compound whose first word is a family name is NOT reported as
//     an id — this is the `command-line` defect, and the assertion fails on the
//     pre-fix reader because `command-line` is matched by modelToken AND
//     separates as an id, so only the table keeps it out;
//  3. a REAL digit-free id of the SAME family IS still reported. This is the
//     half that makes the exclusion token-scoped rather than a family-wide hole:
//     `command-r`, `command-light`, `solar-pro` and `llama-guard` all carry no
//     digit, so they share `command-line`'s shape and would be lost by any rule
//     that treated `<family>-<word>` as prose by default (that rule is the
//     "the tail must carry a digit" filter the guard's header records as the
//     regression of card t_16fa0164 — see api/scripts/biteproof_model_id_scan.py
//     case 9 and the `nested/other_family_nodigit.go` fixture).
func TestProseCompoundExclusionsAreProseAndDoNotShelterIDs(t *testing.T) {
	if len(proseCompounds) == 0 {
		t.Fatal("proseCompounds is empty — if `command-line` and its siblings no longer match the " +
			"tokeniser, delete this table AND this test together; an empty table would read as a " +
			"decision to tolerate prose it does not tolerate")
	}

	// (1) The table is honest.
	for tok, why := range proseCompounds {
		if why == "" {
			t.Errorf("proseCompounds[%q] has an empty reason — an exclusion without a reason is "+
				"indistinguishable from an oversight", tok)
		}
		if tok != strings.ToLower(tok) {
			t.Errorf("proseCompounds[%q] is not lower-case — the lookup lower-cases the token, so a "+
				"mixed-case key would never be reached", tok)
		}
		if !modelToken.MatchString(tok) {
			t.Errorf("proseCompounds[%q] is a token the tokeniser no longer produces — rot: the entry "+
				"excludes a shape that cannot occur, so it excuses nothing and must be dropped", tok)
		}
		if !idHasIdSeparator(tok) {
			t.Errorf("proseCompounds[%q] is not id-SEPARATED, so `idHasIdSeparator` already drops it "+
				"before this table is consulted — the entry is dead weight", tok)
		}
		if strings.ContainsAny(tok, "0123456789") {
			t.Errorf("proseCompounds[%q] carries a DIGIT. A digit-free `<family>-<word>` is the shape "+
				"that is ambiguous between prose and an id; a token with a digit in it looks like an "+
				"id (`command-r7b`, `solar-pro2`) and must be JUDGED, not silenced", tok)
		}
		if strings.Contains(tok, ":") {
			t.Errorf("proseCompounds[%q] carries a `:` — the id:tag spelling is judged by its own "+
				"branch, and prose with a colon there is already refused by isIDWordChar; a `:` entry "+
				"here would silence a shape the guard deliberately keeps", tok)
		}
		if tok == workingModel || strings.HasPrefix(tok, retiredModelFamily) {
			t.Errorf("proseCompounds[%q] is the shipped or retired id — the prose table must never be "+
				"able to excuse a real (or dead) model id", tok)
		}
	}

	// (2)+(3) Verdicts, on a scratch tree.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("prepare fixture tree: %v", err)
	}
	// The English compounds: matched by the tokeniser, separated as ids, and NOT
	// reported. `command-line` is the card's own measured phrase.
	prose := map[string]string{
		"nested/prose_command_line.md":   "// the command-line half of the check\n",
		"nested/prose_solar_powered.md":  "// a solar-powered sensor, and a phi-coefficient\n",
		"nested/prose_granite_falcon.md": "// a granite-faced wall; a falcon-eyed scout\n",
	}
	// The real, digit-free ids of the SAME families: reported.
	ids := map[string]string{
		"nested/command_r.go":     "// The drafter calls command-r via the internal endpoint\n",
		"nested/command_light.go": "// The drafter calls command-light via the internal endpoint\n",
		"nested/command_r7b.go":   "// The drafter calls command-r7b via the internal endpoint\n",
		"nested/solar_pro.go":     "// The drafter calls solar-pro via the internal endpoint\n",
		"nested/llama_guard.go":   "// The drafter calls llama-guard via the internal endpoint\n",
		"nested/gemini_pro.go":    "// The drafter calls gemini-pro via the internal endpoint\n",
	}
	for rel, body := range prose {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatalf("write prose fixture %s: %v", rel, err)
		}
	}
	for rel, body := range ids {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatalf("write id fixture %s: %v", rel, err)
		}
	}

	hits := readTokenFiles(t, root).hits

	for rel := range prose {
		if _, ok := hits[rel]; ok {
			t.Errorf("%s writes only English compounds (`command-line` and friends) and the scan "+
				"reported it as a model id: %v — this is the false positive card t_dba05702 measured "+
				"on a full local gate, and it must not be able to come back", rel, hits[rel])
		}
	}
	for rel := range ids {
		if _, ok := hits[rel]; !ok {
			t.Errorf("%s writes a REAL model id of a family an English word shadows and the scan did "+
				"not report it — the prose exclusion is silencing the family instead of the one "+
				"compound token, which is the regression the digit filter was (card t_16fa0164)", rel)
		}
	}
	if len(hits) != len(ids) {
		var got []string
		for rel := range hits {
			got = append(got, rel)
		}
		sort.Strings(got)
		t.Errorf("the scan reported %d files, want exactly the %d that write a real id: %v — a hit "+
			"the id fixtures did not write means the exclusion is not reaching every compound",
			len(hits), len(ids), got)
	}
}

// blindSpotNote is the per-shape explanation attached to a missed fixture, so a
// future reader sees which half of the old scan went blind rather than a bare
// path.
func blindSpotNote(rel string) string {
	if filepath.Base(rel) == "other_family_nodigit.go" {
		return "the digit filter dropped a LISTED family's id that carries no digit (`<family>-tail` is " +
			"an id) — the shape the pre-widening reader caught for the deepseek family (card t_16fa0164)"
	}
	if filepath.Base(rel) == "colon_tagged.go" {
		return "the separator rule accepted only `-` or a leading digit, so a hyphenless family's " +
			"`<family>:<tag>` id was reached by no branch at all — and unknownModelTag needs a digit " +
			"before the colon, which such a family does not have (card t_492976fe)"
	}
	switch filepath.Ext(rel) {
	case ".mjs":
		return "the old readExts whitelist did not carry .mjs"
	case "":
		return "the old readExts whitelist could not match an extensionless file (filepath.Ext is \"\")"
	case ".txt":
		return "the old readExts whitelist did not carry .txt"
	case ".go", ".md":
		return "the old modelToken matched only ids beginning `deepseek-`, so another family was never tokenised"
	}
	return "the scan did not read this file at all"
}

// repoRoot locates the repository root relative to this test file
// (api/internal/llm -> ../../..).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate the repository root")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// scanResult is what one walk of the repo found, plus the counts the header
// quotes as this guard's scope. A guard that reads nothing must be visible as
// such rather than reading as a clean tree.
type scanResult struct {
	// hits is keyed by repo-relative path, then by the model id the file writes.
	// An id whose value is "" is the SHIPPED default; any other value is the
	// reason it is a stray. Both are kept, because the user-facing surfaces are
	// asserted to MENTION the shipped id, which an omissions-only scan cannot
	// see.
	hits         map[string]map[string]string
	filesRead    int
	filesSkipped int
	// nonText names every file the walk declined to read because its first
	// maxTextProbe bytes are not text (card t_7a1a902c). It is a SUBSET of
	// filesSkipped and it is carried, not counted, because a skip a reader cannot
	// see is the silence this guard exists to remove: scope() names these files,
	// so a tree that loses one to the content rule says so in its own red.
	nonText []string
}

// scope renders the measured scope of the walk for a failure message, so a red
// says how much of the tree it actually read.
func (s scanResult) scope() string {
	msg := "this guard read " + strconv.Itoa(s.filesRead) + " files under the repo root and skipped " +
		strconv.Itoa(s.filesSkipped) + " (*_test.go, the binary skip list and " +
		strconv.Itoa(len(s.nonText)) + " file(s) whose first bytes are not text); there is no extension whitelist."
	if len(s.nonText) > 0 {
		msg += " The non-text files it could not judge, named so the skip cannot hide: " +
			strings.Join(s.nonText, ", ") + "."
	}
	return msg
}

// skipDirs are the trees that are not shipped source. `.next` is on the list
// because a build caches the build shell's own environment (ANTHROPIC_MODEL,
// BRANCH) into `.next/cache` — a harness artefact, not a shipped id, recorded on
// card t_6784f5bb.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".next": true, "vendor": true,
	"test-results": true, ".worktrees": true,
}

// skipReadExts is the residual skip list: formats this scan cannot read as text.
// It is the INVERSE of the whitelist that used to live here, and that inversion
// is the fix for card t_6784f5bb — a new extension is read by default, and only
// something added here is invisible. A file that still cannot be decoded as text
// is reported by name instead of being skipped, so this list cannot grow in
// silence past a file the guard should have read.
var skipReadExts = map[string]bool{
	// images
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".avif": true, ".bmp": true, ".tiff": true, ".tif": true, ".ico": true,
	".icns": true,
	// fonts
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	// archives, prebuilt blobs and object code
	".pdf": true, ".zip": true, ".gz": true, ".tgz": true, ".bz2": true,
	".xz": true, ".zst": true, ".7z": true, ".rar": true, ".tar": true,
	".wasm": true, ".so": true, ".dylib": true, ".dll": true, ".a": true,
	".o": true, ".class": true, ".jar": true, ".exe": true, ".bin": true,
	".db": true, ".sqlite": true, ".sqlite3": true,
	// generated artifacts and dependency metadata
	".lock": true, ".map": true, ".tsbuildinfo": true,
}

// skipReadNames is the same residual rule one level down: a FILE whose NAME makes
// it generated dependency metadata, whatever its extension.
//
// WHY IT EXISTS. `skipReadExts` covers the metadata formats that carry their own
// extension (`.lock`, `.map`), but an npm lockfile is `package-lock.json` — a
// `.json` — so it was read as shipped source, and npm writes BASE64 INTEGRITY
// HASHES into it. That is not a mention of a model, it is a hash: adding the
// eslint toolchain (card t_0e165357) pulled `napi-postinstall@0.3.4`, whose
// integrity is `sha512-PHI5f1O0EP5xJ9gQmFGMS6IZcrVvTjpXjz7Na41gTE7eE2hK11lg04…`,
// and the token `<family>5f1O0EP…` in it red TestUserFacingStringsNameTheSameModelAsTheConstant
// with a message about the shipped LLM default. Measured: the two stray hits
// this scan reported on that tree were exactly this hash (`…PHI5f1O0…`) and the
// package FILENAME `…shebang-command-2.0.0.tgz` (whose `command-2.0.0` needs no
// help from a hash), both introduced by packages the lockfile merely NAMES.
//
// WHY A NAME LIST AND NOT ANOTHER EXTENSION OR A FILE-SCOPED EXEMPTION.
//   - adding `.json` to skipReadExts would drop EVERY JSON file from the scan,
//     `web/public/openapi.json` and the MCP schema descriptions included — a
//     silent narrowing of the one guard whose whole point is to read widely;
//   - a `modelMentionExempt` entry would need the HASH as its id and a reason
//     for it, which is unreadable and would have to be re-written every time
//     npm re-resolves that package (the file is regenerated, not edited);
//   - this list is name-scoped, so it can only ever excuse a file npm generates
//     from a package graph. A model id typed into a `package-lock.json` by hand
//     would be excused too — accepted deliberately, because that file is not a
//     surface the product ships or a user reads, which is exactly the class
//     `.lock`/`.map`/`.tsbuildinfo` already sit in.
var skipReadNames = map[string]bool{
	// npm's own lockfile: integrity hashes and package filenames, regenerated by
	// `npm install`/`npm ci`, never hand-edited (see the measurement above).
	"package-lock.json": true,
}

// scanRepoForModelIDs walks the repo and judges every model id it finds.
func scanRepoForModelIDs(t *testing.T, root string) scanResult {
	t.Helper()
	res := readTokenFiles(t, root)
	hits, read := res.hits, res.filesRead
	if read == 0 {
		t.Fatalf("the walk read no file under %s — a scan that reads nothing proves nothing", root)
	}
	if len(hits) == 0 {
		t.Fatalf("no file under %s writes a model id (read %d files) — either every mention was removed "+
			"(then this guard should be removed with it) or the token rule stopped matching", root, read)
	}
	return res
}

// readTokenFiles is the whole scan: walk, read, tokenise, judge. It returns the
// ids found per file (with the reason when the id is not the shipped default),
// the number of files actually read, the number skipped by the documented rules,
// and — when a skip was decided by CONTENT rather than by name — the paths of the
// non-text files, so `scope()` can name them.
//
// What is read: every file except `*_test.go` (a fixture must not be able to
// satisfy a guard about shipped text), anything on the binary skip list, a file
// whose first maxTextProbe bytes are not text, and the skipped trees. What a hit
// means: every id the file writes, so the caller can assert both directions — no
// stray id anywhere, and the shipped id spelled on the surfaces that must name it.
func readTokenFiles(t *testing.T, root string) scanResult {
	t.Helper()

	res, err := scanTree(root)
	var unreadable *unreadableFileError
	if errors.As(err, &unreadable) {
		t.Fatalf("the scan could not decode %s as text, so it cannot judge the file. Its first %d bytes DID "+
			"decode, so this is NOT one of the compiled build outputs the content rule skips — it is a text "+
			"file with a corrupt region, or source this guard must judge. Fix the bytes rather than widening "+
			"the skip list.", unreadable.path, maxTextProbe)
	}
	var linked *symlinkedTreeError
	if errors.As(err, &linked) {
		t.Fatalf("the scan will not judge %s — %s. This guard reads ONE tree's bytes: following the link would "+
			"let another checkout's contents decide the verdict, and skipping it in silence would narrow the scan "+
			"the way the old extension whitelist did. If the link is not shipped source (a dev tree's hand-linked "+
			"`web/node_modules` is the case this guard met), name it in skipDirs; otherwise put the real bytes in "+
			"this tree.", linked.path, linked.why())
	}
	if err != nil {
		t.Fatalf("walk %s for model ids: %v — the guard cannot run without reading the tree", root, err)
	}
	return res
}

// scanTree is readTokenFiles without the testing hook: it walks root, reads and
// judges every file it should judge, and RETURNS the walk error so the three ways
// this walk can refuse to answer (a text file that stops decoding mid-file, a
// symlinked tree, an unreadable path) can be reported by name at the one place
// that reports, and asserted on a scratch tree without a failing test to read a
// message from.
//
// A non-text file is NOT one of those errors: it is a skip, recorded in
// res.nonText (card t_7a1a902c). It used to be an error, and then a compiled
// binary in the gitignored api/bin killed both repo-wide guards on any machine
// that had built before gating.
func scanTree(root string) (scanResult, error) {
	res := scanResult{hits: map[string]map[string]string{}}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if info.IsDir() {
			if skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		// A SYMLINK IS NEITHER A FILE NOR A DIRECTORY HERE (card t_f7430361).
		// filepath.Walk LSTATs every entry, so a directory reached through a link
		// is not a directory to it: the skipDirs arm above never fires for
		// `web/node_modules`, the entry falls through to the read below, and
		// os.ReadFile answers EISDIR — which aborted BOTH repo-wide guards within
		// seconds of starting, with `read <tree>/web/node_modules: is a directory`, a message that reads
		// like a broken guard and names no product defect. A dev worktree links its
		// `node_modules` by hand to skip an ~81 s `npm ci`; that tree was never
		// seeded, so refusing a symlinked donor in deploy/web_modules.sh (card
		// t_eaf5edd4) does not cover it.
		//
		// The rule, and the two things it must NOT do:
		//   - a link to a DIRECTORY is skipped by the same NAME rule as a real
		//     directory (the guard already intends not to read `node_modules`), and
		//     `nil` is returned rather than filepath.SkipDir: SkipDir from a
		//     non-directory entry skips the REST OF THE CONTAINING DIRECTORY, which
		//     would silently drop the files beside the link;
		//   - any other link to a directory, and a broken link, is REPORTED by name
		//     with its target rather than read (the EISDIR abort) or dropped (silent
		//     narrowing). The walk never descends into a link, so the verdict cannot
		//     come to depend on another checkout's bytes;
		//   - a link to a regular FILE is still read through, which is what this
		//     scan has always done and is not the defect above: a path that serves
		//     readable bytes is read, a tree the walk declines to enter is not.
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(path)
			linked, serr := os.Stat(path)
			switch {
			case serr != nil:
				if skipDirs[name] {
					return nil // not shipped source by name, resolvable or not
				}
				return &symlinkedTreeError{path: relPath(root, path), target: target, broken: true}
			case linked.IsDir():
				if skipDirs[name] {
					return nil
				}
				return &symlinkedTreeError{path: relPath(root, path), target: target}
			}
		}
		rel := relPath(root, path)
		if strings.HasSuffix(name, "_test.go") {
			res.filesSkipped++
			return nil
		}
		if skipReadNames[name] {
			res.filesSkipped++
			return nil
		}
		if skipReadExts[strings.ToLower(filepath.Ext(name))] {
			res.filesSkipped++
			return nil
		}
		// THE CONTENT RULE (card t_7a1a902c). A file whose first maxTextProbe
		// bytes are not text cannot be judged as source, so it is SKIPPED rather
		// than read — and NAMED in res.nonText so scope() reports it. The header
		// records why the decision is by CONTENT and not by path, and the measured
		// case: api/bin/jomform-api, the ELF `make api` writes into a gitignored
		// tree, which killed both repo-wide guards on any machine that built
		// before gating.
		raw, notText, rerr := readIfText(path, rel)
		if rerr != nil {
			return rerr
		}
		if notText {
			res.filesSkipped++
			res.nonText = append(res.nonText, rel)
			return nil
		}
		// A file whose 4 KiB header DECODED but which is not valid UTF-8 past
		// that is REPORTED, not silently dropped: that shape is a text file with
		// a corrupt region rather than build output, and silence here is how the
		// extension whitelist failed.
		if !utf8.Valid(raw) {
			return &unreadableFileError{path: rel}
		}
		res.filesRead++
		text := string(raw)

		ids := map[string]bool{}
		for _, m := range modelToken.FindAllString(text, -1) {
			if !idHasIdSeparator(m) {
				continue // a bare family name in prose is not a model id
			}
			if isProseCompound(m) {
				continue // an English hyphenated compound (e.g. `command-line`), not an id
			}
			ids[m] = true
		}
		// The unknown-family net: an id-shaped token with a size tag whose family
		// modelFamilies does not carry would otherwise never be judged.
		for _, id := range unknownModelTag.FindAllString(text, -1) {
			if modelFamilyOf(id) == "" {
				ids[id] = true
			}
		}
		if len(ids) == 0 {
			return nil
		}

		rel = relPath(root, path)
		found := map[string]string{}
		for id := range ids {
			if id == workingModel {
				found[id] = ""
				continue
			}
			found[id] = strayReason(id)
		}
		res.hits[rel] = found
		return nil
	})
	return res, err
}

// maxTextProbe is how much of a file the scan reads before deciding whether the
// file is source at all (card t_7a1a902c). 4 KiB is the whole of nearly every file
// this guard judges, and for the ones it is not, it is enough to recognise a
// compiled binary: measured on the 79.6 MB ELF `make api` writes into api/bin,
// byte 96 is already invalid UTF-8 and the first 4 KiB hold 3315 NUL bytes.
const maxTextProbe = 4096

// readIfText reads a file's bytes for judging, and reports whether the file is NOT
// text — in which case the returned bytes are nil and the caller skips it.
//
// WHY A PROBE AND NOT os.ReadFile. The old walk read the WHOLE file and only then
// discovered it could not decode it, so a 79.6 MB binary was pulled into memory on
// every repo-wide guard — and the guard then died on it rather than skipping it.
// Reading at most maxTextProbe first costs 4 KiB for a build output instead of its
// own size, and a file that fits inside the probe is returned WITHOUT a second
// read.
//
// WHAT "TEXT" MEANS HERE, and why both halves are needed. A NUL byte cannot occur
// in a UTF-8 file this repo ships, and a compiled binary carries them in its header
// whatever its other bytes look like (the ELF e_ident padding is NULs) — so NUL is
// what catches an image whose header happens to be ASCII. Invalid UTF-8 catches the
// rest. The two are one rule rather than two: both say "this is not text, so it
// cannot be judged", and neither can be turned into a reason to read an image.
func readIfText(path, rel string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", rel, err)
	}
	defer f.Close()

	probe := make([]byte, maxTextProbe)
	n, rerr := io.ReadFull(f, probe)
	switch rerr {
	case nil, io.ErrUnexpectedEOF, io.EOF:
	default:
		return nil, false, fmt.Errorf("read %s: %w", rel, rerr)
	}
	probe = probe[:n]

	if !probeIsText(probe, n == maxTextProbe) {
		return nil, true, nil
	}
	if n < maxTextProbe {
		return probe, false, nil // the probe held the whole file
	}
	rest, rerr := io.ReadAll(f)
	if rerr != nil {
		return nil, false, fmt.Errorf("read %s: %w", rel, rerr)
	}
	return append(probe, rest...), false, nil
}

// probeIsText reports whether a file's leading bytes can be read as text.
// `truncated` says the probe was filled to maxTextProbe, i.e. the file continues
// past it.
//
// The only forgiveness it grants is at the probe's own boundary: a multi-byte rune
// the 4 KiB read CUT IN HALF makes an otherwise valid file look invalid, so a
// trailing INCOMPLETE rune is dropped before the file is called non-text. An
// invalid byte anywhere else is real, and a NUL is never forgiven.
func probeIsText(probe []byte, truncated bool) bool {
	if bytes.IndexByte(probe, 0) >= 0 {
		return false
	}
	if utf8.Valid(probe) {
		return true
	}
	if !truncated {
		return false
	}
	// Walk back to the start of the rune the read may have cut — at most UTFMax-1
	// bytes — and accept the file as text only when what is left of that rune is a
	// valid PREFIX of one (a lead byte with room for at least one more continuation
	// byte) and everything before it decodes. A complete-but-invalid sequence, or
	// an invalid leading byte, is a real defect rather than a cut.
	for cut := 1; cut < utf8.UTFMax && cut <= len(probe); cut++ {
		start := len(probe) - cut
		if !utf8.RuneStart(probe[start]) {
			continue // a continuation byte of the cut rune: look further left
		}
		want := runePrefixLen(probe[start])
		if want == 0 || cut >= want {
			return false
		}
		for _, c := range probe[start+1:] {
			if c < 0x80 || c > 0xBF {
				return false
			}
		}
		return utf8.Valid(probe[:start])
	}
	return false
}

// runePrefixLen returns how many bytes a multi-byte UTF-8 sequence starts with, or
// 0 when b is not a lead byte (ASCII, a continuation byte, or an invalid lead such
// as 0xC0/0xC1/0xF5..).
func runePrefixLen(b byte) int {
	switch {
	case b >= 0xC2 && b <= 0xDF:
		return 2
	case b >= 0xE0 && b <= 0xEF:
		return 3
	case b >= 0xF0 && b <= 0xF4:
		return 4
	}
	return 0
}

// relPath renders path relative to root, slash-separated, for both the hits map
// and the messages that name a file — so a red and the map it is about spell a
// path the same way.
func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// unreadableFileError names a file the scan found but could not read as text.
type unreadableFileError struct{ path string }

func (e *unreadableFileError) Error() string { return "not text: " + e.path }

// symlinkedTreeError names a link the walk declines to judge: a link to a
// directory whose name is not on skipDirs, or a link whose target is gone. It is
// an ERROR rather than a silent skip because a directory the scan refuses to
// enter is exactly the narrowing the extension whitelist used to hide.
type symlinkedTreeError struct {
	path   string // repo-relative, for the message
	target string // the link's own target, as written
	broken bool   // the target could not be resolved at all
}

func (e *symlinkedTreeError) Error() string { return e.path + " is " + e.why() }

// why renders the half of the sentence that depends on the link's state, so the
// two refusals read as what they are rather than as one generic walk error.
func (e *symlinkedTreeError) why() string {
	if e.broken {
		return "a BROKEN symlink (its target " + e.target + " does not exist), so there is no tree to judge"
	}
	return "a symlink to the directory " + e.target + ", and this walk does not follow a link into another tree"
}

// idHasIdSeparator reports whether a family token is followed by an id SEPARATOR:
// `-` (the shape every hyphenated id uses — `deepseek-coder`, `gemini-pro`,
// `claude-opus`), a leading DIGIT (the `qwen3` shape), or a `:` with an
// id-shaped tail (the `id:tag` spelling — `gpt-oss:120b`, `mistral:7b`). A bare
// family word in prose carries none of the three, which is the only thing this
// test is here to reject — `phi` and `solar` are English words.
//
// WHY THE `:` BRANCH IS LOAD-BEARING. The two other branches cannot reach a
// family that carries no hyphen: the first requires the `-` itself, the second a
// leading digit. `gpt-oss:120b` — the id this guard's own header records as
// measured LIVE — is therefore judged by nothing without it, and `unknownModelTag`
// cannot cover it either because that net needs a digit before the colon (card
// t_492976fe). The tail after the `:` must be id-shaped (at least one word
// character), so a family name followed by a colon and a space (`phi: a word`)
// stays prose.
//
// WHY NOT "CONTAINS A DIGIT". That was the previous rule, and it is a different
// statement: it drops a real id that merely has no digit in it (`deepseek-coder`),
// which the reader before this widening DID catch — so the filter was a
// regression, not a narrowing of prose noise (card t_16fa0164). The separator,
// not the digit, is what distinguishes an id from a family name.
//
// The digit fallback is unreachable by construction — modelToken only matches a
// token that starts with one of modelFamilies — and is kept as the conservative
// answer rather than `false` so an unforeseen token shape is still judged instead
// of being silently dropped.
func idHasIdSeparator(id string) bool {
	low := strings.ToLower(id)
	for _, fam := range modelFamilies {
		if !strings.HasPrefix(low, fam) {
			continue
		}
		rest := low[len(fam):]
		if rest == "" {
			return false
		}
		switch {
		case rest[0] == '-' || (rest[0] >= '0' && rest[0] <= '9'):
			return true
		case rest[0] == ':':
			// A colon with something id-shaped after it. `phi: a word` carries a
			// space there and stays prose.
			return len(rest) > 1 && isIDWordChar(rest[1])
		}
		return false
	}
	return strings.ContainsAny(id, "0123456789")
}

// isIDWordChar reports whether b can be the first character of an id's tail
// after a `:`. Deliberately narrow: a space, a `)` or a sentence period must not
// turn a family name in prose into an id.
func isIDWordChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '.' || b == '_' || b == '-'
}

// modelFamilyOf returns the family modelFamilies carries for an id, or "" when
// none does. The family must be followed by a non-letter so `phi` in prose is not
// read as the `phi` family, and the list is longest-first so `gpt-oss` wins over
// `gpt`.
func modelFamilyOf(id string) string {
	low := strings.ToLower(id)
	for _, fam := range modelFamilies {
		if !strings.HasPrefix(low, fam) {
			continue
		}
		rest := low[len(fam):]
		if rest == "" || rest[0] < 'a' || rest[0] > 'z' {
			return fam
		}
	}
	return ""
}

// strayReason explains WHY an id that is not the shipped default is a red. The
// shapes fail for different reasons, and the message has to say which one it is:
// "the provider stopped serving this" and "this is a different model somebody
// chose" call for different actions.
func strayReason(id string) string {
	if strings.HasPrefix(id, retiredModelFamily) {
		return "RETIRED family: the provider answers HTTP 410 Gone for every id in " + retiredModelFamily +
			" (measured on the tagged id, retired 2026-09-25), which is the outage this guard was written for"
	}
	if modelFamilyOf(id) == "" {
		return "no provider family on this guard's list covers this id (it carries a model SIZE tag, so it is " +
			"an id rather than prose): if it is a real provider family, add it to modelFamilies on purpose; " +
			"otherwise point the mention at " + workingModel
	}
	return "family " + modelFamilyOf(id) + " is not the shipped default: the provider serves several families " +
		"(gpt-oss:120b and " + workingModel + " measured HTTP 200) and has retired others (qwen3-coder:480b, " +
		"deepseek-v3.1:671b measured HTTP 410 on 2026-09-28), so a mention of any other id is a live risk. " +
		"Point it at " + workingModel + ", or make the change a deliberate one in this guard"
}

// descriptionPattern pulls `Name: "x"` … `Description: "y"` pairs (tools.go) or
// `{ name: "x", desc: "y" }` pairs (the docs page) into a name→description map.
var (
	goDescriptionPattern   = regexp.MustCompile(`(?s)Name:\s*"([a-z0-9_]+)",\s*Description:\s*"((?:[^"\\]|\\.)*)"`)
	pageDescriptionPattern = regexp.MustCompile(`(?s)\{\s*name:\s*"([a-z0-9_]+)",\s*desc:\s*"((?:[^"\\]|\\.)*)"`)
)

// toolDescriptions returns the tool name→description pairs held by the MCP tool
// REGISTRY — every non-test .go file in mcpRegistryDir, in sorted order.
//
// WHY EVERY FILE, and what it changes. Today this parse finds all 181 pairs
// across the package regardless of whether it reads one file or all of them
// (tools.go holds 180, parity_issue396.go holds `disable_2fa`), so the widening
// is not cosmetic. It is the difference between the two failure modes the card
// named:
//
//   - a literal that MOVES: read narrowly, the reader still sees the copy left
//     behind in tools.go and compares the docs page against a STALE sentence —
//     green, and wrong. Read package-wide, the moved copy is what is compared;
//   - a name that LEAVES tools.go: read narrowly, the missing pair is a fatal
//     that blames tools.go for a tool that is still registered elsewhere. Read
//     package-wide, it is a fatal only when the name is gone from the registry,
//     which is the claim worth making.
//
// One pass per file, not one over joined text, and a file holding the same name
// twice is a failure rather than a last-write-wins: a scan that cannot tell
// which pair it read cannot say the page matches the shipped sentence.
//
// A file with NO pairs is not a failure here, and that is deliberate: the
// registry is the model-mention list (mcpToolFiles), and a file may legitimately
// mention the model in a doc comment while holding no literal — legal_mcp.go and
// form_generate.go do exactly that. Per-file emptiness is only a failure for the
// single-file readers below, where it means the pattern matched nothing.
func toolDescriptions(t *testing.T, hits map[string]map[string]string, root string) map[string]string {
	t.Helper()
	files := mcpToolFiles(t, hits)
	if len(files) == 0 {
		t.Fatalf("%s holds no non-test .go file that writes a model id — the tool descriptions the docs "+
			"must match have moved or been rewritten, and an empty registry would pass vacuously",
			mcpRegistryDir)
	}
	out := map[string]string{}
	from := map[string]string{}
	for _, rel := range files {
		pairs, err := parseDescriptions(filepath.Join(root, rel), goDescriptionPattern)
		if err != nil {
			t.Fatalf("read %s: %v — the docs-parity half of this guard cannot run without the registry", rel, err)
		}
		for name, desc := range pairs {
			if _, dup := out[name]; dup {
				t.Fatalf("%s holds the %q tool description twice (%s and %s) — the docs-parity check reads "+
					"one pair per name and cannot say which of the two the page matches", mcpRegistryDir,
					name, from[name], rel)
			}
			out[name] = desc
			from[name] = rel
		}
	}
	if len(out) == 0 {
		t.Fatalf("no tool name/description pair parsed from any of the %d files in %s — the registry "+
			"reader matched nothing, so the docs-parity assertion would pass vacuously",
			len(files), mcpRegistryDir)
	}
	return out
}

// mcpToolFiles is the registry's file list, from the SAME repo walk that already
// found the model ids: every non-test .go file directly inside mcpRegistryDir,
// sorted, each of which must write a model id.
//
// The walk is reused rather than the directory re-listed on purpose. It makes
// "this file is in the registry" and "this file names the model" the same read
// of the tree, so a file that joins the package without naming the model cannot
// sit in the registry half-unchecked — the two halves cannot drift into
// disagreeing about what the registry is.
//
// The direct-children restriction mirrors the two web guards (`mcpsrv/*.go`): the
// package is flat, and a file in a SUBdirectory is a different package whose
// literals this server does not register.
//
// The hit map's VALUE type is the scan's (file -> id -> reason) rather than a
// bare set, because this reader and the scan must read the tree ONCE: the keys
// are the files that wrote a model id, which is exactly the list wanted here, and
// a second walk with its own shape is how the two halves would drift apart about
// what the registry is. Only the keys are read.
func mcpToolFiles(t *testing.T, hits map[string]map[string]string) []string {
	t.Helper()
	prefix := mcpRegistryDir + "/"
	var files []string
	for rel := range hits {
		if !strings.HasPrefix(rel, prefix) {
			continue
		}
		if strings.Contains(strings.TrimPrefix(rel, prefix), "/") {
			t.Fatalf("%s saw %s, which is not a direct child of the registry package — this reader walks "+
				"one level and would silently ignore the file's literals, so it refuses the tree instead "+
				"of under-reading it", mcpRegistryDir, rel)
		}
		if filepath.Ext(rel) != ".go" {
			continue
		}
		files = append(files, rel)
	}
	sort.Strings(files)
	return files
}

// docsToolDesc is one docs entry plus the file it was read from, so a failure
// names the file the sentence actually lives in.
type docsToolDesc struct {
	desc string
	file string // repo-relative
}

// docsSurfaceFiles is every repo-relative file that can carry a docs tool entry:
// the five /docs/mcp pages plus the module they render the rows from.
func docsSurfaceFiles() []string {
	out := make([]string, 0, len(docsMCPPageRelPaths)+1)
	out = append(out, docsMCPPageRelPaths...)
	return append(out, docsMCPToolGroupsRelPath)
}

// docsPageDescriptions reads the tool `{ name, desc }` entries from the WHOLE
// /docs/mcp surface and returns them keyed by tool name.
//
// WHY THE SET, NOT ONE PAGE (card t_ee4114af). This reader used to name
// web/src/app/docs/mcp/page.tsx, which was right while /docs/mcp was one page.
// Card t_0356ed4c split it into a hub plus four task pages and moved the rows into
// the module those pages import; the hub now holds neither a row nor the model
// mention, so a single-file reader went red on a tree where every user-facing
// surface still named the model. Reading the union fixes that without weakening
// the rule: the entry still has to exist, and it still has to match the registry.
//
// A file with NO entries is not a failure here — four of the six files legitimately
// hold prose only. A NAME HELD IN TWO FILES IS a failure (the same rule the
// single-file reader enforced inside one file): the reader keys by name, so it
// cannot say which of two rendered entries the registry was compared against.
func docsPageDescriptions(t *testing.T, root string) map[string]docsToolDesc {
	t.Helper()

	out := map[string]docsToolDesc{}
	for _, rel := range docsSurfaceFiles() {
		pairs, err := parseDescriptions(filepath.Join(root, rel), pageDescriptionPattern)
		if err != nil {
			t.Fatalf("read %s: %v — the docs-parity half of this guard cannot run without the "+
				"whole /docs/mcp surface", rel, err)
		}
		for name, desc := range pairs {
			if prev, dup := out[name]; dup {
				t.Fatalf("the %q tool description appears on TWO files of the /docs/mcp surface "+
					"(%s and %s) — this reader keys by name, so the comparison below would judge one "+
					"of two user-visible entries. Remove one row", name, prev.file, rel)
			}
			out[name] = docsToolDesc{desc: desc, file: rel}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no tool name/description pair parsed from any file of the /docs/mcp surface "+
			"(looked in: %s) — the pattern matched nothing, so the docs-parity assertion would "+
			"pass vacuously", strings.Join(docsSurfaceFiles(), ", "))
	}
	return out
}

// parseDescriptions reads one file and returns what the pattern found in it,
// without judging emptiness — the callers decide whether an empty result is a
// vacuous check. A file that cannot be READ is an error rather than an empty
// parse: the previous single-file reader fataled on it and a registry file that
// vanishes mid-scan must not silently shrink the registry.
//
// A NAME HELD TWICE IS AN ERROR, on the page side too (card t_ce63518c). This map
// is keyed by tool name, so two `{ name, desc }` rows for one tool used to
// collapse to whichever the regex matched LAST — silently, while the registry
// reader below refused the same shape. Measured on the page: inserting a stale
// duplicate `generate_form` row BEFORE the real one left this guard GREEN, and
// the docs page then served two cards for one tool with two different sentences
// (`TOOL_GROUPS` is rendered directly). A comparison against the registry is only
// worth making if the reader can say WHICH row it read, so it now refuses and
// names both positions.
func parseDescriptions(path string, re *regexp.Regexp) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(raw)
	out := map[string]string{}
	first := map[string]int{}
	for _, idx := range re.FindAllStringSubmatchIndex(content, -1) {
		name := content[idx[2]:idx[3]]
		if prev, ok := first[name]; ok {
			return nil, fmt.Errorf("%s holds the %q tool description twice (line %d and line %d) — "+
				"this reader keys by name, so the second row would silently win and the comparison "+
				"below would judge one of two user-visible cards. Remove one row",
				path, name, lineOf(content, prev), lineOf(content, idx[0]))
		}
		first[name] = idx[0]
		out[name] = unescapeUnicode(content[idx[4]:idx[5]])
	}
	return out, nil
}

// lineOf reports the 1-based line a byte offset falls on.
func lineOf(s string, off int) int {
	if off > len(s) {
		off = len(s)
	}
	return strings.Count(s[:off], "\n") + 1
}

// unescapeUnicode resolves the `\uXXXX` escapes a TSX string literal may use for
// a character the Go source spells directly, so the two can be compared.
var unicodeEscape = regexp.MustCompile(`\\u([0-9a-fA-F]{4})`)

func unescapeUnicode(s string) string {
	return unicodeEscape.ReplaceAllStringFunc(s, func(m string) string {
		var r rune
		for _, c := range []byte(m[2:]) {
			r <<= 4
			switch {
			case c >= '0' && c <= '9':
				r += rune(c - '0')
			case c >= 'a' && c <= 'f':
				r += rune(c-'a') + 10
			case c >= 'A' && c <= 'F':
				r += rune(c-'A') + 10
			}
		}
		return string(r)
	})
}
