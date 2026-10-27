// Runtime credentials for the courier adapters.
//
// WHERE THEY COME FROM. Two files outside the repo, read at runtime and never
// copied into code, tests, commits, logs or the web bundle:
//
//	/root/.config/jomform/lalamove-sandbox.env   (pk_test_/sk_test_ — SANDBOX)
//	/root/.config/jomform/easyparcel-oauth.env   (OAuth app; env is per-account)
//
// A MISSING FILE IS NOT AN ERROR. Production Lalamove keys (pk_prod_) and the
// live EasyParcel account are Wan's to obtain; until he does, the correct state
// is "this provider is not configured", which must be a clear, non-crashing
// answer the dashboard can show and the submit path can fall back from. So
// absence produces an empty config, never a fatal error and never a panic.
//
// THE VALUES NEVER LEAVE THIS FILE'S STRUCTS. Nothing here is logged, echoed,
// returned in JSON or put in an error message; the only thing that is ever
// reported is WHERE a credential was read from (a path is configuration, not a
// secret). A credential that is present but unusable (an empty value) is treated
// as absent.
package shipping

import (
	"os"
	"strings"
)

// CourierCredentials is the runtime credential set for the courier adapters.
type CourierCredentials struct {
	Lalamove   LalamoveConfig
	EasyParcel EasyParcelConfig
	// LalamovePath / EasyParcelPath record the file each config was read from
	// ("" when the path was not configured).
	LalamovePath   string
	EasyParcelPath string
	// LalamoveSource describes where Lalamove values came from: "file",
	// "env", "file+env" or "none". Same for EasyParcel. It is what the connect
	// surface shows an operator, and it carries no value.
	LalamoveSource   string
	EasyParcelSource string
}

// LoadCourierCredentials reads both credential sources. lalamovePath and
// easyparcelPath are the env-file paths (empty = not configured); values already
// present in the process environment override the file, so a container that is
// handed a single mounted file (the staging shape) works exactly like the host.
func LoadCourierCredentials(lalamovePath, easyparcelPath string) CourierCredentials {
	var c CourierCredentials
	c.LalamovePath = strings.TrimSpace(lalamovePath)
	c.EasyParcelPath = strings.TrimSpace(easyparcelPath)

	llFile := readEnvFile(c.LalamovePath)
	c.Lalamove = LalamoveConfig{
		Key:    pick(os.Getenv("LALAMOVE_API_KEY"), llFile["LALAMOVE_SANDBOX_API_KEY"]),
		Secret: pick(os.Getenv("LALAMOVE_API_SECRET"), llFile["LALAMOVE_SANDBOX_API_SECRET"]),
		Base:   pick(os.Getenv("LALAMOVE_API_BASE"), llFile["LALAMOVE_SANDBOX_BASE"]),
		Market: pick(os.Getenv("LALAMOVE_MARKET"), llFile["LALAMOVE_MARKET"]),
	}
	c.LalamoveSource = sourceLabel(len(llFile) > 0 && c.Lalamove.Configured(), envHasAny(
		"LALAMOVE_API_KEY", "LALAMOVE_API_SECRET", "LALAMOVE_API_BASE", "LALAMOVE_MARKET"))

	epFile := readEnvFile(c.EasyParcelPath)
	c.EasyParcel = EasyParcelConfig{
		ClientID:     pick(os.Getenv("EASYPARCEL_CLIENT_ID"), epFile["EASYPARCEL_CLIENT_ID"]),
		ClientSecret: pick(os.Getenv("EASYPARCEL_CLIENT_SECRET"), epFile["EASYPARCEL_CLIENT_SECRET"]),
		AuthorizeURL: pick(os.Getenv("EASYPARCEL_AUTHORIZE_URL"), epFile["EASYPARCEL_AUTHORIZE_URL"]),
		TokenURL:     pick(os.Getenv("EASYPARCEL_TOKEN_URL"), epFile["EASYPARCEL_TOKEN_URL"]),
		RedirectStaging: pick(os.Getenv("EASYPARCEL_REDIRECT_STAGING"),
			epFile["EASYPARCEL_REDIRECT_STAGING"]),
		RedirectProd: pick(os.Getenv("EASYPARCEL_REDIRECT_PROD"),
			epFile["EASYPARCEL_REDIRECT_PROD"]),
		APIBase:    pick(os.Getenv("EASYPARCEL_API_BASE"), epFile["EASYPARCEL_API_BASE"]),
		APIVersion: pick(os.Getenv("EASYPARCEL_API_VERSION"), epFile["EASYPARCEL_API_VERSION"]),
	}
	c.EasyParcelSource = sourceLabel(len(epFile) > 0 && c.EasyParcel.Configured(), envHasAny(
		"EASYPARCEL_CLIENT_ID", "EASYPARCEL_CLIENT_SECRET"))

	// Production Lalamove keys arrive as env vars only (the file stays the
	// sandbox pair); prefer them when present so a prod key is never shadowed
	// by a sandbox file.
	if k := strings.TrimSpace(os.Getenv("LALAMOVE_PROD_API_KEY")); k != "" {
		c.Lalamove.Key = k
		c.Lalamove.Secret = strings.TrimSpace(os.Getenv("LALAMOVE_PROD_API_SECRET"))
		c.Lalamove.Base = pick(strings.TrimSpace(os.Getenv("LALAMOVE_PROD_API_BASE")), LalamoveProdBase)
		c.LalamoveSource = strings.Trim(c.LalamoveSource+",env(prod)", ",")
	}
	return c
}

// pick returns the first non-blank value (trimmed).
func pick(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// envHasAny reports whether any of the named env vars is set to a non-blank value.
func envHasAny(names ...string) bool {
	for _, n := range names {
		if strings.TrimSpace(os.Getenv(n)) != "" {
			return true
		}
	}
	return false
}

// sourceLabel describes where a credential set came from, without a value.
func sourceLabel(fromFile, fromEnv bool) string {
	switch {
	case fromFile && fromEnv:
		return "file+env"
	case fromFile:
		return "file"
	case fromEnv:
		return "env"
	default:
		return "none"
	}
}

// readEnvFile parses a KEY=VALUE env file. A missing or unreadable file yields an
// empty map — absence is a state, not an error (see the file comment). Values are
// trimmed and surrounding quotes removed; comments (#) and blank lines ignored.
func readEnvFile(path string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(path) == "" {
		return out
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		if k == "" {
			continue
		}
		out[k] = v
	}
	return out
}
