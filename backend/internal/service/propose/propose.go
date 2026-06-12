// Package propose holds helpers shared by the concrete proposers (cli, api):
// the prompt we send to an LLM and the parser that turns its reply into a
// domain.Proposal. Keeping these here means the local-CLI proposer and the
// hosted-API proposer ask for — and parse — exactly the same thing.
package propose

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/trudoan/query-optimizer/internal/domain"
)

// ParseProposal extracts a single JSON object from an LLM reply. Models often
// wrap output in prose or ```json fences, so we slice from the first '{' to the
// last '}' before unmarshalling. OriginalSQL and SelfServe are NOT trusted from
// the model — the caller fills them deterministically.
func ParseProposal(out []byte) (domain.Proposal, error) {
	s := string(out)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return domain.Proposal{}, fmt.Errorf("no JSON object in output: %q", strings.TrimSpace(s))
	}
	var p domain.Proposal
	if err := json.Unmarshal([]byte(s[start:end+1]), &p); err != nil {
		return domain.Proposal{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if p.Kind == "" {
		return domain.Proposal{}, fmt.Errorf("proposal missing kind")
	}
	return p, nil
}

// BuildPrompt renders the self-contained instruction sent to the model: the
// schema, the rules (rewrite is the only auto-provable kind; never fake a
// speedup), the slow query + plan, and — on a retry — the prior failure.
func BuildPrompt(in domain.ProposeInput) string {
	var b strings.Builder
	b.WriteString("You are a SQL query optimizer. Analyze the slow query and its EXPLAIN plan, ")
	b.WriteString("then propose ONE optimization as a single JSON object. Output ONLY the JSON — no prose, no markdown fences.\n\n")

	b.WriteString("JSON schema:\n")
	b.WriteString("{\n")
	b.WriteString(`  "kind": "rewrite" | "index" | "partition" | "shard",` + "\n")
	b.WriteString(`  "rewritten_sql": "<full SQL, REQUIRED when kind=rewrite, must return the SAME rows>",` + "\n")
	b.WriteString(`  "ddl": "<CREATE INDEX ... text, for kind=index/partition; it is NEVER executed by us>",` + "\n")
	b.WriteString(`  "rationale": "<plain-language why this helps>"` + "\n")
	b.WriteString("}\n\n")

	b.WriteString("Rules:\n")
	b.WriteString("- Prefer a behavior-preserving rewrite (kind=rewrite) when one exists — it is the only kind we can prove automatically.\n")
	b.WriteString("- A rewrite MUST return the identical result set; we run old vs new and reject any row difference.\n")
	b.WriteString("- If the real fix is a new index, use kind=index and put the CREATE INDEX in \"ddl\". Do NOT rewrite the query just to fake a speedup.\n")
	b.WriteString("- Use kind=partition / kind=shard only when that is genuinely the bottleneck.\n")
	b.WriteString("- Never invent columns or tables not present in the plan.\n\n")

	fmt.Fprintf(&b, "Engine: %s\n\n", in.Engine)
	fmt.Fprintf(&b, "Slow query:\n%s\n\n", in.OriginalSQL)
	fmt.Fprintf(&b, "EXPLAIN plan:\n%s\n", in.Plan)

	if in.Attempt > 0 && in.PriorVerdict != nil {
		b.WriteString("\n--- RETRY ---\n")
		fmt.Fprintf(&b, "Your previous rewrite was REJECTED (status: %s).\n", in.PriorVerdict.Status)
		if in.PriorRewrite != "" {
			fmt.Fprintf(&b, "Previous rewrite:\n%s\n", in.PriorRewrite)
		}
		if len(in.PriorVerdict.Notes) > 0 {
			fmt.Fprintf(&b, "Reason: %s\n", strings.Join(in.PriorVerdict.Notes, "; "))
		}
		b.WriteString("Produce a corrected proposal.\n")
	}
	return b.String()
}
