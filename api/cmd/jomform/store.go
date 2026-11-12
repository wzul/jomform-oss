package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// storedToken is this machine's own copy of the grant. `status` and `logout`
// read it, so neither needs the harness config to describe itself.
//
// The file is 0600 and nothing in this package ever renders its token fields.
type storedToken struct {
	Server       string    `json:"server"`
	MCPURL       string    `json:"mcp_url"`
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Scope        string    `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
	AccountName  string    `json:"account_name,omitempty"`
	AccountSlug  string    `json:"account_slug,omitempty"`
	HarnessPath  string    `json:"harness_path,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// storedClient is the dynamically registered client_id, kept across logins so a
// re-run does not register a new client every time. It is not secret: the
// server issues public clients.
type storedClient struct {
	Server     string `json:"server"`
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name,omitempty"`
}

func (c *cli) configDir() string {
	return filepath.Join(c.home, ".config", "jomform")
}

func (c *cli) tokenPath() string {
	return filepath.Join(c.configDir(), "token.json")
}

func (c *cli) clientPath() string {
	return filepath.Join(c.configDir(), "client.json")
}

func (c *cli) loadToken() (storedToken, error) {
	var t storedToken
	raw, err := os.ReadFile(c.tokenPath())
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return t, fmt.Errorf("%s is not a token file this command wrote", c.tokenPath())
	}
	if t.AccessToken == "" || t.Server == "" {
		return t, fmt.Errorf("%s is missing the server or the token", c.tokenPath())
	}
	return t, nil
}

func (c *cli) saveToken(t storedToken) error {
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return writeFile0600(c.tokenPath(), append(raw, '\n'))
}

func (c *cli) loadClient() (storedClient, error) {
	var cl storedClient
	raw, err := os.ReadFile(c.clientPath())
	if err != nil {
		return cl, err
	}
	if err := json.Unmarshal(raw, &cl); err != nil {
		return cl, err
	}
	if cl.ClientID == "" {
		return cl, fmt.Errorf("%s holds no client_id", c.clientPath())
	}
	return cl, nil
}

func (c *cli) saveClient(cl storedClient) error {
	raw, err := json.MarshalIndent(cl, "", "  ")
	if err != nil {
		return err
	}
	return writeFile0600(c.clientPath(), append(raw, '\n'))
}
