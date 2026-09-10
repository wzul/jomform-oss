// Package llm provides a thin client for Ollama Cloud content generation
// (issue #184). It is used to auto-generate per-workspace legal pages (privacy /
// terms / refund) from a structured prompt template filled with the merchant's
// business details. The merchant reviews and edits the generated text before
// saving, so the output is a draft, not a final legal document.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultModel is the Ollama Cloud model used for legal-page generation
// (same model family as the chatwoot-ai decision).
const DefaultModel = "deepseek-v4-flash:0731"

// DefaultBaseURL is the Ollama Cloud OpenAI-compatible API root.
const DefaultBaseURL = "https://ollama.com/v1"

// Client talks to Ollama Cloud's OpenAI-compatible /chat/completions endpoint.
type Client struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
}

// New builds an Ollama Cloud client. Empty baseURL/model use the defaults.
// The API key is read from the OLLAMA_API_KEY env var (Bearer auth on the
// OpenAI-compatible endpoint). Without it Ollama returns 401 Unauthorized.
func New(baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		baseURL: baseURL,
		model:   model,
		apiKey:  os.Getenv("OLLAMA_API_KEY"),
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// chatRequest is the OpenAI-compatible chat completion request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the OpenAI-compatible chat completion response body.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// GenerateLegalPage asks the model to draft a legal policy of the given type
// for the merchant's business. The prompt template fills in the merchant's
// business details so the draft is specific and sensible. The returned text is
// a draft for the merchant to review/edit before saving.
func (c *Client) GenerateLegalPage(ctx context.Context, pageType string, d BusinessDetails) (string, error) {
	prompt := buildPrompt(pageType, d)
	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: "You are a legal-drafting assistant for small Malaysian online sellers. Write clear, plain-language policy text in English. Use simple headings and short paragraphs. Do not invent facts that were not provided — where a detail is missing, write a neutral placeholder the merchant can fill in. Output only the policy body text, no preamble."},
			{Role: "user", Content: prompt},
		},
		Stream: false,
	})
	if err != nil {
		return "", err
	}
	return c.chat(ctx, body)
}

// ClassifySpam asks the model to judge whether a form submission is spam or
// legit (issue #245). The prompt carries the submitted field values, email,
// IP and form context. The model replies with a single word: SPAM or LEGIT.
// The returned bool is true when the submission is spam. A non-SPAM/LEGIT
// reply is treated as legit (fail-open: never block a real submission).
func (c *Client) ClassifySpam(ctx context.Context, submission string) (bool, error) {
	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: "You are a spam classifier for online form submissions. Classify the submission as SPAM or LEGIT. Reply with exactly one word: SPAM or LEGIT. Do not add any other text."},
			{Role: "user", Content: submission},
		},
		Stream: false,
	})
	if err != nil {
		return false, err
	}
	reply, err := c.chat(ctx, body)
	if err != nil {
		return false, err
	}
	upper := strings.ToUpper(strings.TrimSpace(reply))
	return strings.HasPrefix(upper, "SPAM"), nil
}

// ClassifyAsset asks the model to classify an uploaded code asset
// (CSS/JS/HTML/SVG) as MALICIOUS / BENIGN / NEEDS REVIEW (issue #364). The
// prompt carries the asset's content and a trimmed code sample so the model
// can judge whether the code is meant to harm visitors (data theft, phishing,
// script injection, obfuscated malware) vs. a legitimate custom landing page.
// The reply is one of the three words. The scan worker (internal/assetgate)
// persists the verdict on the media row's scan_status, and the asset is NOT
// served until it classifies as benign.
func (c *Client) ClassifyAsset(ctx context.Context, code string) (string, error) {
	// Trim to a bounded slice so a huge file doesn't blow the prompt / cost.
	if len(code) > 12000 {
		code = code[:12000]
	}
	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: "You are a security classifier for static website assets uploaded to a form builder. Users upload CSS/JS/HTML/SVG to style a landing page. Classify the asset as MALICIOUS if it is meant to harm visitors (script injection, data theft, phishing, keylogging, malvertising, obfuscated malware) or to abuse the platform as a malicious-script host. Classify as BENIGN if it is ordinary, safe website styling/behaviour (layout, fonts, form validation, analytics that the owner opted into). Classify as NEEDS REVIEW if it is an unusual pattern that a human should inspect before serving (external script loads, unusual obfuscation, suspicious data exfiltration). Reply with exactly one word: MALICIOUS, BENIGN, or NEEDS REVIEW. Do not add any other text."},
			{Role: "user", Content: "Asset content:\n" + code},
		},
		Stream: false,
	})
	if err != nil {
		return "", err
	}
	reply, err := c.chat(ctx, body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reply), nil
}

// GenerateFormConfig asks the model to draft a JomForm form config (blocks +
// styling) from a natural-language prompt (issue #256). The prompt constrains
// the model to the known block types and the JSON shape the formgen package
// parses. The returned text is a draft for the merchant to review/edit before
// saving — it is validated server-side before it becomes a form.
func (c *Client) GenerateFormConfig(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: "You are a structured JSON generator. Always reply with exactly one valid JSON object and nothing else."},
			{Role: "user", Content: prompt},
		},
		Stream: false,
	})
	if err != nil {
		return "", err
	}
	return c.chat(ctx, body)
}

// chat posts a chat-completion request body and returns the trimmed reply.
func (c *Client) chat(ctx context.Context, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	// Guard against non-JSON responses (e.g. an HTML landing page served by a
	// misconfigured endpoint). json.Unmarshal would otherwise fail with a
	// confusing "invalid character '<'" error.
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		return "", fmt.Errorf("llm: unexpected response Content-Type %q (expected application/json); body: %s", ct, strings.TrimSpace(string(raw)))
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("llm: response is not valid JSON: %w", err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", fmt.Errorf("llm: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("llm: empty response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

// BusinessDetails carries the merchant's business facts used to fill the
// prompt template.
type BusinessDetails struct {
	BusinessName     string `json:"business_name"`
	RegistrationNo   string `json:"registration_no"`
	Contact          string `json:"contact"`
	Address          string `json:"address"`
	DataCollected    string `json:"data_collected"`
	PaymentTerms     string `json:"payment_terms"`
	RefundTerms      string `json:"refund_terms"`
	Website          string `json:"website"`
	EffectiveDate    string `json:"effective_date"`
}

// buildPrompt assembles the structured prompt template for a page type.
func buildPrompt(pageType string, d BusinessDetails) string {
	var title string
	switch pageType {
	case "privacy":
		title = "Privacy Policy"
	case "terms":
		title = "Terms of Service"
	case "refund":
		title = "Refund Policy"
	default:
		title = "Policy"
	}
	var sb strings.Builder
	sb.WriteString("Draft a " + title + " for the following business. Fill in the details below where relevant.\n\n")
	sb.WriteString("Business name: " + orPlaceholder(d.BusinessName) + "\n")
	sb.WriteString("Registration number: " + orPlaceholder(d.RegistrationNo) + "\n")
	sb.WriteString("Contact: " + orPlaceholder(d.Contact) + "\n")
	sb.WriteString("Address: " + orPlaceholder(d.Address) + "\n")
	sb.WriteString("Website: " + orPlaceholder(d.Website) + "\n")
	sb.WriteString("Effective date: " + orPlaceholder(d.EffectiveDate) + "\n")
	switch pageType {
	case "privacy":
		sb.WriteString("What data is collected: " + orPlaceholder(d.DataCollected) + "\n")
	case "terms":
		sb.WriteString("Payment terms: " + orPlaceholder(d.PaymentTerms) + "\n")
	case "refund":
		sb.WriteString("Refund terms: " + orPlaceholder(d.RefundTerms) + "\n")
	}
	sb.WriteString("\nWrite the policy as a series of numbered sections with a short heading for each. Keep it concise and practical for a small online seller.")
	return sb.String()
}

func orPlaceholder(s string) string {
	if strings.TrimSpace(s) == "" {
		return "[not provided — leave a neutral placeholder]"
	}
	return strings.TrimSpace(s)
}
