// Package api implements port.Proposer by calling a hosted LLM API directly,
// using a key the SERVER holds. This is the "hosted mode" proposer: the public
// webapp runs one fixed demo database and pays for the model itself, so a user
// with no Claude/Cursor CLI can still get a proposal. Contrast with the cli
// proposer, which piggybacks the user's own local CLI login (no server key).
//
// It speaks the Anthropic Messages API. The key is read from the environment
// and never logged or returned in errors.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/trudoan/query-optimizer/internal/domain"
	"github.com/trudoan/query-optimizer/internal/service/propose"
)

const (
	defaultBaseURL = "https://api.anthropic.com/v1/messages"
	defaultModel   = "claude-sonnet-4-6"
	anthropicVer   = "2023-06-01"
	maxTokens      = 2048
)

// Proposer calls a hosted LLM API to produce a Proposal.
type Proposer struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// New reads $QOPT_API_KEY (required) and optional $QOPT_API_MODEL /
// $QOPT_API_BASE_URL. Returns an error if no key is set — the caller (deps)
// decides whether hosted mode is usable.
func New() (*Proposer, error) {
	key := os.Getenv("QOPT_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("hosted proposer needs QOPT_API_KEY; none set")
	}
	model := os.Getenv("QOPT_API_MODEL")
	if model == "" {
		model = defaultModel
	}
	base := os.Getenv("QOPT_API_BASE_URL")
	if base == "" {
		base = defaultBaseURL
	}
	return &Proposer{
		apiKey:  key,
		model:   model,
		baseURL: base,
		client:  &http.Client{Timeout: 120 * time.Second},
	}, nil
}

// Name identifies the proposer in results.
func (p *Proposer) Name() string { return "api:" + p.model }

// request / response mirror the subset of the Anthropic Messages API we use.
type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type response struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Propose sends the prompt to the API and parses one Proposal from the reply.
func (p *Proposer) Propose(ctx context.Context, in domain.ProposeInput) (domain.Proposal, error) {
	body, err := json.Marshal(request{
		Model:     p.model,
		MaxTokens: maxTokens,
		Messages:  []message{{Role: "user", Content: propose.BuildPrompt(in)}},
	})
	if err != nil {
		return domain.Proposal{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(body))
	if err != nil {
		return domain.Proposal{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVer)

	resp, err := p.client.Do(req)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()

	var out response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return domain.Proposal{}, fmt.Errorf("decode api response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := "unknown error"
		if out.Error != nil {
			msg = out.Error.Message
		}
		return domain.Proposal{}, fmt.Errorf("api status %d: %s", resp.StatusCode, msg)
	}

	var text strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}

	prop, err := propose.ParseProposal([]byte(text.String()))
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("parse api output: %w", err)
	}
	prop.OriginalSQL = in.OriginalSQL
	prop.SelfServe = prop.Kind == domain.KindRewrite
	return prop, nil
}
