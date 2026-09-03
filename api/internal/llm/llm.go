// Package llm provides a thin client for Ollama Cloud content generation
// (issue #184). It is used to auto-generate per-space legal pages (privacy /
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
	"strings"
	"time"
)

// DefaultModel is the Ollama Cloud model used for legal-page generation
// (same model family as the chatwoot-ai decision).
const DefaultModel = "deepseek-v4-flash:0731"

// DefaultBaseURL is the Ollama Cloud API root.
const DefaultBaseURL = "https://ollama.cloud/api"

// Client talks to Ollama Cloud's OpenAI-compatible /api/chat endpoint.
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// New builds an Ollama Cloud client. Empty baseURL/model use the defaults.
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

// chat posts a chat-completion request body and returns the trimmed reply.
func (c *Client) chat(ctx context.Context, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
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
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
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
