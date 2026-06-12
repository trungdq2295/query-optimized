// Package cli implements port.Proposer by spawning the user's own installed
// agent CLI (Claude Code or Cursor Agent) headless. This is the "local mode"
// proposer: it piggybacks the user's existing CLI login, so the backend needs
// NO LLM API key of its own. The CLI is run text-in / JSON-out — we feed it the
// slow query + its EXPLAIN plan and ask for exactly one Proposal as JSON.
//
// It is deliberately NOT agentic (no file edits, no tool calls): a single
// stdin->stdout turn keeps the seam dumb and the security boundary intact —
// the only thing that reaches the database is the verifier, never the CLI.
package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/trudoan/query-optimizer/internal/domain"
	"github.com/trudoan/query-optimizer/internal/service/propose"
)

// Proposer drives an installed agent CLI as a subprocess.
type Proposer struct {
	path  string // absolute path to the CLI binary
	name  string // "cli:claude" | "cli:cursor"
	model string // optional --model override (empty = CLI default)
}

// New detects an agent CLI and returns a Proposer. Detection order:
//  1. $QOPT_AGENT_CLI (explicit path or bare name on PATH)
//  2. claude  (Claude Code)
//  3. cursor-agent / agent  (Cursor Agent)
//
// $QOPT_AGENT_MODEL optionally pins a model (e.g. "sonnet"). Returns an error
// if no CLI is found — the caller (deps) decides whether that is fatal.
func New() (*Proposer, error) {
	model := os.Getenv("QOPT_AGENT_MODEL")

	candidates := []string{"claude", "cursor-agent", "agent"}
	if pref := os.Getenv("QOPT_AGENT_CLI"); pref != "" {
		candidates = append([]string{pref}, candidates...)
	}

	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			return &Proposer{path: path, name: "cli:" + family(path), model: model}, nil
		}
	}
	return nil, fmt.Errorf("no agent CLI found on PATH (tried claude, cursor-agent, agent); set QOPT_AGENT_CLI")
}

// family maps a binary path to a short proposer family for Name().
func family(path string) string {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(base, "claude") {
		return "claude"
	}
	return "cursor"
}

// Name identifies the proposer in results (e.g. "cli:claude").
func (p *Proposer) Name() string { return p.name }

// Propose feeds the prompt to the CLI over stdin and parses one Proposal from
// its stdout. The CLI never touches the database — it only produces text.
func (p *Proposer) Propose(ctx context.Context, in domain.ProposeInput) (domain.Proposal, error) {
	prompt := propose.BuildPrompt(in)

	cmd := exec.CommandContext(ctx, p.path, p.args()...)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return domain.Proposal{}, fmt.Errorf("agent CLI %q failed: %w (stderr: %s)",
			p.name, err, strings.TrimSpace(stderr.String()))
	}

	prop, err := propose.ParseProposal(stdout.Bytes())
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("parse %q output: %w", p.name, err)
	}

	// The CLI may omit fields we already know or that must be derived — fill
	// them deterministically rather than trusting the model.
	prop.OriginalSQL = in.OriginalSQL
	prop.SelfServe = prop.Kind == domain.KindRewrite
	return prop, nil
}

// args builds the CLI flags for headless text mode.
//   - claude:        -p   (print mode; reads prompt from stdin)
//   - cursor/agent:  --trust -p   (auto-trust workspace, print mode)
func (p *Proposer) args() []string {
	var a []string
	if p.model != "" {
		a = append(a, "--model", p.model)
	}
	if family(p.path) == "cursor" {
		a = append(a, "--trust")
	}
	a = append(a, "-p")
	return a
}
