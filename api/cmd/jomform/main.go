// Command jomform is the end-user CLI for a JomForm account.
//
// It exists for the one OAuth hop a browser-less harness cannot make by itself.
// The device authorization grant (RFC 8628) starts when the CLIENT posts to the
// device endpoint; an MCP host that implements only authorization_code + PKCE
// never calls it, so it falls back to "open a browser and sign in". This command
// performs that grant in a terminal and writes the resulting credential where a
// harness already looks for its MCP servers.
//
//	jomform mcp login --headless [--server https://jomform.com] [--out <path>]
//	jomform mcp status
//	jomform mcp logout
//
// Nothing here prints a token, a device code or a client secret. stdout carries
// the verification URL, the user code, the expiry in words and, once the
// credential is stored, only the config path. Diagnostics go to stderr with a
// non-zero exit.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// defaultServer is the hosted authorization server. --server exists for a
	// self-hosted deployment; the loopback check in device.go keeps a mistyped
	// plain-http URL from carrying a bearer token over the wire.
	defaultServer = "https://jomform.com"
	// defaultMCPURL is the remote MCP endpoint a harness is pointed at.
	defaultMCPURL = "https://mcp.jomform.com/mcp"
	// defaultClientName is what the consent screen and the dashboard call this
	// client when the user does not pass --client-name.
	defaultClientName = "JomForm CLI"
	// defaultScope is the narrowest grant the ladder offers. A harness onboarding
	// starts read-only; widening is a separate, deliberate command.
	defaultScope = "read"
)

// cli carries the process-wide dependencies. Tests substitute every one of them
// (home, clock, sleep, HTTP client, account lookup), so the flow is exercised
// without touching a real home directory, a real network or a real clock.
type cli struct {
	stdout io.Writer
	stderr io.Writer
	home   string
	now    func() time.Time
	sleep  func(time.Duration)
	http   *http.Client

	// lookupAccount reads the account name behind a freshly issued token. It is
	// best effort: the credential is already valid when it runs, so a failure
	// costs the status line's account, never the login.
	lookupAccount func(ctx context.Context, mcpURL, accessToken string) (accountRef, error)
}

func newCLI(stdout, stderr io.Writer) *cli {
	home, _ := os.UserHomeDir()
	return &cli{
		stdout:        stdout,
		stderr:        stderr,
		home:          home,
		now:           time.Now,
		sleep:         time.Sleep,
		http:          &http.Client{Timeout: 30 * time.Second},
		lookupAccount: lookupAccount,
	}
}

func main() {
	c := newCLI(os.Stdout, os.Stderr)
	if err := c.run(os.Args[1:]); err != nil {
		if errors.Is(err, errUsage) {
			fmt.Fprintln(c.stderr, "jomform: "+err.Error())
			fmt.Fprint(c.stderr, usageText)
			os.Exit(2)
		}
		fmt.Fprintln(c.stderr, "jomform: "+err.Error())
		os.Exit(1)
	}
}

// errUsage marks a malformed command line, which exits 2 (the conventional
// "you asked for something that does not exist" status) rather than 1.
var errUsage = errors.New("usage")

const usageText = `usage:
  jomform mcp login --headless [--server URL] [--client-name NAME] [--scope read|readwrite]
                             [--email ADDRESS] [--out PATH] [--mcp-url URL] [--force]
  jomform mcp status [--out PATH]
  jomform mcp logout [--out PATH] [--force]
`

func (c *cli) run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: a command is required", errUsage)
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(c.stdout, usageText)
		return nil
	case "mcp":
		if len(args) < 2 {
			return fmt.Errorf("%w: mcp needs one of login, status or logout", errUsage)
		}
		switch args[1] {
		case "login":
			return c.cmdLogin(args[2:])
		case "status":
			return c.cmdStatus(args[2:])
		case "logout":
			return c.cmdLogout(args[2:])
		default:
			return fmt.Errorf("%w: unknown mcp command %q", errUsage, args[1])
		}
	default:
		return fmt.Errorf("%w: unknown command %q (try `jomform help`)", errUsage, args[0])
	}
}

// loginOptions is the parsed `jomform mcp login` command line.
type loginOptions struct {
	server     string
	mcpURL     string
	clientName string
	scope      string
	email      string
	out        string
	force      bool
}

func (c *cli) cmdLogin(args []string) error {
	o := loginOptions{}
	fs := flag.NewFlagSet("jomform mcp login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	headless := fs.Bool("headless", false, "finish the device authorization grant in this terminal")
	fs.StringVar(&o.server, "server", defaultServer, "authorization server base URL")
	fs.StringVar(&o.mcpURL, "mcp-url", "", "remote MCP endpoint to write into the harness config")
	fs.StringVar(&o.clientName, "client-name", defaultClientName, "name shown on the consent screen")
	fs.StringVar(&o.scope, "scope", defaultScope, "requested scope: read or readwrite")
	fs.StringVar(&o.email, "email", "", "address to send the approval code to (optional login_hint)")
	fs.StringVar(&o.out, "out", "", "harness MCP config to update")
	fs.BoolVar(&o.force, "force", false, "write to a config path outside the known harness locations")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	if !*headless {
		return fmt.Errorf("%w: this CLI only implements the headless device flow — re-run with --headless", errUsage)
	}

	server, err := normalizeServer(o.server)
	if err != nil {
		return err
	}
	mcpURL, err := resolveMCPURL(o.mcpURL, server)
	if err != nil {
		return err
	}
	// The path is validated BEFORE the user is asked to approve anything: a
	// refused path must not cost a device code and a trip to a phone.
	out, err := c.resolveHarnessPath(o.out)
	if err != nil {
		return err
	}
	if !o.force && !c.pathAllowed(out) {
		return fmt.Errorf("%s is not a harness config this command knows; pass --force to write there anyway", out)
	}
	return c.login(server, mcpURL, out, o)
}

// login runs the grant, stores the credential and updates the harness config.
func (c *cli) login(server, mcpURL, out string, o loginOptions) error {
	ctx := context.Background()

	clientID, err := c.ensureClient(ctx, server, o.clientName)
	if err != nil {
		return err
	}
	grant, err := c.startDevice(ctx, server, clientID, o)
	if err != nil {
		return err
	}

	// The whole of stdout before the credential exists: where to go, the code to
	// read out, and how long the two are good for. Nothing else.
	fmt.Fprintln(c.stdout, grant.VerificationURIComplete)
	fmt.Fprintln(c.stdout, grant.UserCode)
	fmt.Fprintf(c.stdout, "open this link and press Approve — you have %s\n", expiryWords(grant.ExpiresIn))

	interval := time.Duration(grant.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ttl := time.Duration(grant.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	tok, err := c.pollToken(ctx, server, clientID, grant.DeviceCode, interval, c.now().Add(ttl))
	if err != nil {
		return err
	}

	// Best effort: the account name is what `status` shows beside the expiry, and
	// a harness that cannot answer must not undo an approved login.
	acct, _ := c.lookupAccount(ctx, mcpURL, tok.AccessToken)

	stored := storedToken{
		Server:       server,
		MCPURL:       mcpURL,
		ClientID:     clientID,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenType:    tok.TokenType,
		Scope:        tok.Scope,
		ExpiresAt:    c.now().Add(time.Duration(tok.ExpiresIn) * time.Second),
		AccountName:  acct.Name,
		AccountSlug:  acct.Slug,
		HarnessPath:  out,
		CreatedAt:    c.now(),
	}
	if err := c.saveToken(stored); err != nil {
		return fmt.Errorf("the token was issued but could not be stored: %w", err)
	}

	entry, err := harnessEntryJSON(mcpURL, tok.AccessToken)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(out)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", out, err)
	}
	merged, err := mergeServer(data, serverName, entry)
	if err != nil {
		return fmt.Errorf("%s was not modified: %w", out, err)
	}
	if err := writeFile0600(out, merged); err != nil {
		return fmt.Errorf("the token is stored, but %s could not be written: %w", out, err)
	}
	// Only the path: the credential is the one thing that must never reach a
	// terminal, a scrollback buffer or a CI log.
	fmt.Fprintln(c.stdout, out)
	return nil
}

func (c *cli) cmdStatus(args []string) error {
	out := ""
	fs := flag.NewFlagSet("jomform mcp status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&out, "out", "", "also report whether this harness config carries the jomform entry")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	tok, err := c.loadToken()
	if err != nil {
		return errors.New("not signed in on this machine — run `jomform mcp login --headless`")
	}
	expired := !c.now().Before(tok.ExpiresAt)
	fmt.Fprintf(c.stdout, "expires: %s\n", tok.ExpiresAt.Local().Format("2006-01-02 15:04 MST"))
	fmt.Fprintf(c.stdout, "scopes: %s\n", displayScope(tok.Scope))
	fmt.Fprintf(c.stdout, "account: %s\n", displayAccount(tok.AccountName, tok.AccountSlug))
	if out != "" {
		path, perr := c.resolveHarnessPath(out)
		if perr != nil {
			return perr
		}
		state := "no " + serverName + " entry"
		if data, rerr := os.ReadFile(path); rerr == nil {
			if hasHarnessEntry(data, serverName) {
				state = serverName + " entry present"
			}
		} else if !os.IsNotExist(rerr) {
			return rerr
		}
		fmt.Fprintf(c.stdout, "harness: %s (%s)\n", path, state)
	}
	if expired {
		return fmt.Errorf("the access token expired at %s — run `jomform mcp login --headless` again",
			tok.ExpiresAt.Local().Format("2006-01-02 15:04 MST"))
	}
	return nil
}

func (c *cli) cmdLogout(args []string) error {
	out := ""
	force := false
	fs := flag.NewFlagSet("jomform mcp logout", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&out, "out", "", "harness MCP config to clean up")
	fs.BoolVar(&force, "force", false, "edit a config path outside the known harness locations")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	tok, err := c.loadToken()
	if err != nil {
		return errors.New("not signed in on this machine — nothing to revoke")
	}
	// Resolve and vet the config path BEFORE revoking: a refused path must not
	// leave a dead token on disk with a "run logout again" instruction on screen.
	path := ""
	if out == "" {
		out = tok.HarnessPath
	}
	if out != "" {
		p, perr := c.resolveHarnessPath(out)
		if perr != nil {
			return perr
		}
		// A --force login may have written somewhere unusual; that is the one
		// path logout may edit without asking again, because the user already
		// said yes to it once.
		alreadyAgreed := tok.HarnessPath != "" && canonicalPath(p) == canonicalPath(tok.HarnessPath)
		if !force && !alreadyAgreed && !c.pathAllowed(p) {
			return fmt.Errorf("%s is not a harness config this command knows; pass --force to edit it anyway", p)
		}
		path = p
	}

	if err := c.revoke(context.Background(), tok); err != nil {
		return fmt.Errorf("the server did not confirm the revocation, so the local copy was kept: %w", err)
	}
	fmt.Fprintln(c.stdout, "revoked the token")

	if path != "" {
		removed, rerr := removeHarnessEntry(path, serverName)
		switch {
		case rerr != nil:
			// The credential is already dead server-side; the config entry left
			// behind is inert, and the user needs to know exactly which file to fix.
			fmt.Fprintf(c.stderr, "jomform: %s was left as it is: %v\n", path, rerr)
		case removed:
			fmt.Fprintf(c.stdout, "removed the %s entry from %s\n", serverName, path)
		default:
			fmt.Fprintf(c.stdout, "no %s entry in %s\n", serverName, path)
		}
	}
	if err := os.Remove(c.tokenPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("the token was revoked, but the local copy could not be removed: %w", err)
	}
	return nil
}

// displayScope renders the granted scope set the way the server advertised it.
func displayScope(scope string) string {
	if strings.TrimSpace(scope) == "" {
		return "(none recorded)"
	}
	return scope
}

// displayAccount renders the account `status` reports. A login whose MCP lookup
// failed still has a usable credential, so the line says so rather than lying.
func displayAccount(name, slug string) string {
	switch {
	case name != "" && slug != "":
		return fmt.Sprintf("%s (%s)", name, slug)
	case name != "":
		return name
	case slug != "":
		return slug
	default:
		return "(the server did not report one)"
	}
}

// expiryWords turns expires_in into the phrase the third stdout line carries.
func expiryWords(seconds int) string {
	if seconds <= 0 {
		return "a moment"
	}
	minutes := (seconds + 59) / 60
	if minutes == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", minutes)
}

// resolveHarnessPath expands a --out argument (or the default) to an absolute
// path. An empty argument means the Cursor config, the one harness file whose
// location is stable across the desktop platforms.
func (c *cli) resolveHarnessPath(arg string) (string, error) {
	if strings.TrimSpace(arg) == "" {
		return filepath.Join(c.home, ".cursor", "mcp.json"), nil
	}
	p, err := expandHome(arg, c.home)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%s is not an absolute path", arg)
	}
	return filepath.Clean(p), nil
}

// pathAllowed reports whether a path is one of the harness configs this command
// knows how to merge into. The known list is the guard: a typo in --out must not
// rewrite an unrelated JSON file, so anything else needs --force.
func (c *cli) pathAllowed(path string) bool {
	want := canonicalPath(path)
	for _, allowed := range c.harnessPaths() {
		if canonicalPath(allowed) == want {
			return true
		}
	}
	return false
}

// harnessPaths is the allowlist. Claude Desktop's file moved between platforms,
// so both documented locations are listed; Cursor's is the same on every one.
func (c *cli) harnessPaths() []string {
	return []string{
		filepath.Join(c.home, ".cursor", "mcp.json"),
		filepath.Join(c.home, "Library", "Application Support", "Claude", "claude_desktop_config.json"),
		filepath.Join(c.home, ".config", "Claude", "claude_desktop_config.json"),
	}
}
