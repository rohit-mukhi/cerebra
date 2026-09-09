package cerebra

import (
	"context"
	"fmt"
	"testing"
)

func TestPrompt620ByteScore(t *testing.T) {
	// Exact prompt BuildPrompt generates for a non-chat issue task (620 bytes)
	// issue: "What is the boiling point of water at sea level?"
	prompt := "You are running as a local coding agent for a Multica workspace.\n\n" +
		"Your assigned issue ID is: 01a07aad-98c8-7101-9c24-8ea4db4503ec\n\n" +
		"Start by running `multica issue get 01a07aad-98c8-7101-9c24-8ea4db4503ec --output json` to understand your task, then complete it.\n" +
		"For comment history, workflow step 2 applies. Scan the threads first with `multica issue comment list 01a07aad-98c8-7101-9c24-8ea4db4503ec --roots-only --summary --compact --output json`, then expand only what matters with `--thread <thread-id> --tail 30`. For `--since` incremental polling, pagination, and folding, see `multica issue comment list --help`.\n\n" +
		"What is the boiling point of water at sea level?"

	fmt.Printf("Prompt length: %d bytes\n", len(prompt))

	h := HeuristicClassifier{}
	tier, rule, _ := h.Score(context.Background(), prompt, TaskMeta{WillUseMCPTools: false})
	fmt.Printf("Tier: %s  Rule: %s\n", tier, rule)

	if tier != TierSimple {
		t.Errorf("simple question should be TierSimple, got %s (rule=%s)", tier, rule)
	}
}
