package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// serverName is the mcpServers key this command owns. Any other key in the file
// belongs to the user and is never rewritten.
const serverName = "jomform"

// errNotJSON marks a harness config that would be damaged by a merge.
var errNotJSON = errors.New("the file is not valid JSON")

// harnessEntryJSON builds the mcpServers member: the remote endpoint plus the
// bearer header the client sends on every call.
func harnessEntryJSON(mcpURL, accessToken string) (json.RawMessage, error) {
	entry := struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}{
		URL:     mcpURL,
		Headers: map[string]string{"Authorization": "Bearer " + accessToken},
	}
	return json.Marshal(entry)
}

// mergeServer writes the `name` member into a config document WITHOUT
// re-serialising the rest of it.
//
// THE CONTRACT IS BYTE PRESERVATION. A harness config is a file we do not own:
// it carries other MCP servers, comments some editors tolerate, and the user's
// own spacing. Decoding into map[string]any and encoding again would reorder
// keys, drop unknown formatting and rewrite every other server's entry. So this
// function locates the byte span of `mcpServers` (and of our member inside it)
// and splices text in; every byte outside that span is copied through untouched.
func mergeServer(data []byte, name string, entry json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return []byte(fmt.Sprintf("{\n  \"mcpServers\": {\n    %s: %s\n  }\n}\n",
			quote(name), indentEntry(entry, "    "))), nil
	}
	if !json.Valid(data) {
		return nil, errNotJSON
	}
	root, err := rootObjectStart(data)
	if err != nil {
		return nil, err
	}

	// Locate `mcpServers` in the root object.
	_, msValStart, _, msFound, _, err := objectMember(data, root, "mcpServers")
	if err != nil {
		return nil, err
	}
	if !msFound {
		_, insertAt, hasMembers, err := objectLayout(data, root)
		if err != nil {
			return nil, err
		}
		text := ",\n  \"mcpServers\": {\n    " + quote(name) + ": " + indentEntry(entry, "    ") + "\n  }"
		if !hasMembers {
			// An empty root object: the text replaces the gap after `{` and is
			// closed by the object's own brace.
			text = "\n  \"mcpServers\": {\n    " + quote(name) + ": " + indentEntry(entry, "    ") + "\n  }\n"
		}
		out := splice(data, insertAt, insertAt, text)
		if !json.Valid(out) {
			return nil, errNotJSON
		}
		return out, nil
	}
	if data[msValStart] != '{' {
		return nil, fmt.Errorf("mcpServers in the config is not an object")
	}

	// Our member may already be there (a second login): replace just its value.
	_, valStart, valEnd, found, _, err := objectMember(data, msValStart, name)
	if err != nil {
		return nil, err
	}
	if found {
		out := splice(data, valStart, valEnd, indentEntry(entry, "    "))
		if !json.Valid(out) {
			return nil, errNotJSON
		}
		return out, nil
	}

	// Append a member, keeping every existing byte where it was.
	_, insertAt, hasMembers, err := objectLayout(data, msValStart)
	if err != nil {
		return nil, err
	}
	text := ",\n    " + quote(name) + ": " + indentEntry(entry, "    ")
	if !hasMembers {
		text = "\n    " + quote(name) + ": " + indentEntry(entry, "    ") + "\n  "
	}
	out := splice(data, insertAt, insertAt, text)
	if !json.Valid(out) {
		return nil, errNotJSON
	}
	return out, nil
}

// withoutServer removes our member. Every other byte is preserved, including
// the other servers' entries.
func withoutServer(data []byte, name string) ([]byte, bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return data, false, nil
	}
	if !json.Valid(data) {
		return nil, false, errNotJSON
	}
	root, err := rootObjectStart(data)
	if err != nil {
		return nil, false, err
	}
	_, msValStart, _, msFound, _, err := objectMember(data, root, "mcpServers")
	if err != nil || !msFound || data[msValStart] != '{' {
		return data, false, err
	}
	keyStart, _, valEnd, found, _, err := objectMember(data, msValStart, name)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return data, false, nil
	}

	// Take the comma BEFORE the member when there is one, otherwise the
	// whitespace that follows the opening brace; then the comma after the value
	// when the member was not the last one.
	start := keyStart
	p := keyStart - 1
	for p >= 0 && isJSONSpace(data[p]) {
		p--
	}
	if p >= 0 && data[p] == ',' {
		start = p
	} else {
		start = msValStart + 1
	}
	end := valEnd
	if start == msValStart+1 {
		// Nothing was consumed on the left, so the separator on the right has to
		// go too; otherwise the previous member and the next one would touch.
		n := valEnd
		for n < len(data) && isJSONSpace(data[n]) {
			n++
		}
		if n < len(data) && data[n] == ',' {
			end = n + 1
		}
	}
	out := splice(data, start, end, "")
	if !json.Valid(out) {
		// Refuse rather than hand back a config the harness cannot parse.
		return nil, false, fmt.Errorf("%s holds a shape this command could not edit safely", "the config")
	}
	return out, true, nil
}

// hasHarnessEntry reports whether the document already carries our member.
func hasHarnessEntry(data []byte, name string) bool {
	if !json.Valid(data) {
		return false
	}
	root, err := rootObjectStart(data)
	if err != nil {
		return false
	}
	_, msStart, _, msFound, _, err := objectMember(data, root, "mcpServers")
	if err != nil || !msFound || data[msStart] != '{' {
		return false
	}
	_, _, _, found, _, err := objectMember(data, msStart, name)
	return err == nil && found
}

// removeHarnessEntry removes our member from a harness config. A missing file or
// a missing member is not an error: there is simply nothing to remove.
func removeHarnessEntry(path, name string) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	out, removed, err := withoutServer(data, name)
	if err != nil || !removed {
		return removed, err
	}
	if err := writeFile0600(path, out); err != nil {
		return false, err
	}
	return true, nil
}

// writeFile0600 replaces a file with 0600 permissions via a temp file in the
// same directory, so a crash cannot leave a half-written config behind.
func writeFile0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".jomform-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// expandHome resolves a leading `~/` against the CLI's home directory.
func expandHome(p, home string) (string, error) {
	switch {
	case p == "~":
		return home, nil
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:]), nil
	case strings.HasPrefix(p, "~"):
		return "", fmt.Errorf("%s uses a form of ~ this command does not expand", p)
	}
	return p, nil
}

// canonicalPath resolves a config path for comparison, following a symlinked
// parent directory so a link cannot smuggle a write outside the allowlist.
func canonicalPath(p string) string {
	p = filepath.Clean(p)
	if real, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		return filepath.Join(real, filepath.Base(p))
	}
	return p
}

// indentEntry renders the mcpServers value with the indentation a config file
// normally uses. It is applied only to OUR entry, never to the rest of the file.
func indentEntry(entry json.RawMessage, prefix string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, entry, prefix, "  "); err != nil {
		return string(entry)
	}
	return buf.String()
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func splice(data []byte, start, end int, text string) []byte {
	out := make([]byte, 0, len(data)-(end-start)+len(text))
	out = append(out, data[:start]...)
	out = append(out, text...)
	out = append(out, data[end:]...)
	return out
}

func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func skipJSONSpace(data []byte, i int) int {
	for i < len(data) && isJSONSpace(data[i]) {
		i++
	}
	return i
}

// rootObjectStart returns the offset of the document's opening brace.
func rootObjectStart(data []byte) (int, error) {
	i := skipJSONSpace(data, 0)
	if i >= len(data) || data[i] != '{' {
		return 0, errNotJSON
	}
	return i, nil
}

// objectMember finds a key in the object starting at objStart and reports the
// byte span of its value. It does not decode or re-encode anything: the spans
// are what the splice is built from.
func objectMember(data []byte, objStart int, key string) (keyStart, valStart, valEnd int, found bool, objEnd int, err error) {
	i := skipJSONSpace(data, objStart)
	if i >= len(data) || data[i] != '{' {
		return 0, 0, 0, false, 0, errNotJSON
	}
	i++
	for {
		i = skipJSONSpace(data, i)
		if i >= len(data) {
			return 0, 0, 0, false, 0, errNotJSON
		}
		if data[i] == '}' {
			return 0, 0, 0, false, i, nil
		}
		start := i
		after, serr := scanString(data, i)
		if serr != nil {
			return 0, 0, 0, false, 0, serr
		}
		thisKey := string(data[start+1 : after-1])
		i = skipJSONSpace(data, after)
		if i >= len(data) || data[i] != ':' {
			return 0, 0, 0, false, 0, errNotJSON
		}
		i = skipJSONSpace(data, i+1)
		vStart := i
		vEnd, verr := scanValue(data, i)
		if verr != nil {
			return 0, 0, 0, false, 0, verr
		}
		if thisKey == key {
			return start, vStart, vEnd, true, -1, nil
		}
		i = skipJSONSpace(data, vEnd)
		if i >= len(data) {
			return 0, 0, 0, false, 0, errNotJSON
		}
		switch data[i] {
		case ',':
			i++
		case '}':
			return 0, 0, 0, false, i, nil
		default:
			return 0, 0, 0, false, 0, errNotJSON
		}
	}
}

// objectLayout reports where the object's closing brace is and where a member
// appended at the end would begin.
func objectLayout(data []byte, objStart int) (closeBrace, insertAt int, hasMembers bool, err error) {
	i := skipJSONSpace(data, objStart)
	if i >= len(data) || data[i] != '{' {
		return 0, 0, false, errNotJSON
	}
	i++
	insertAt = i
	hasMembers = false
	for {
		i = skipJSONSpace(data, i)
		if i >= len(data) {
			return 0, 0, false, errNotJSON
		}
		if data[i] == '}' {
			return i, insertAt, hasMembers, nil
		}
		after, serr := scanString(data, i)
		if serr != nil {
			return 0, 0, false, serr
		}
		i = skipJSONSpace(data, after)
		if i >= len(data) || data[i] != ':' {
			return 0, 0, false, errNotJSON
		}
		i = skipJSONSpace(data, i+1)
		end, verr := scanValue(data, i)
		if verr != nil {
			return 0, 0, false, verr
		}
		insertAt = end
		hasMembers = true
		i = skipJSONSpace(data, end)
		if i >= len(data) {
			return 0, 0, false, errNotJSON
		}
		switch data[i] {
		case ',':
			i++
		case '}':
			return i, insertAt, hasMembers, nil
		default:
			return 0, 0, false, errNotJSON
		}
	}
}

// scanString returns the offset just past the closing quote of the string that
// starts at i.
func scanString(data []byte, i int) (int, error) {
	if i >= len(data) || data[i] != '"' {
		return 0, errNotJSON
	}
	i++
	for i < len(data) {
		switch data[i] {
		case '\\':
			i += 2
		case '"':
			return i + 1, nil
		default:
			i++
		}
	}
	return 0, errNotJSON
}

// scanValue returns the offset just past the value that starts at i.
func scanValue(data []byte, i int) (int, error) {
	if i >= len(data) {
		return 0, errNotJSON
	}
	switch data[i] {
	case '{', '[':
		depth := 0
		for i < len(data) {
			switch data[i] {
			case '"':
				after, err := scanString(data, i)
				if err != nil {
					return 0, err
				}
				i = after
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1, nil
				}
			}
			i++
		}
		return 0, errNotJSON
	case '"':
		return scanString(data, i)
	default:
		start := i
		for i < len(data) && !isJSONSpace(data[i]) && data[i] != ',' && data[i] != '}' && data[i] != ']' {
			i++
		}
		if i == start {
			return 0, errNotJSON
		}
		return i, nil
	}
}
