package daemon

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebra"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestBuildOpenclawTierMap_SingleAgent(t *testing.T) {
	// Your real openclaw setup: one agent "main" backed by gemini-3.6-flash
	orig := listModels
	listModels = func(_ context.Context, provider string, _ agent.Command) (agent.Catalog, error) {
		return agent.Catalog{Models: []agent.Model{
			{
				ID:       "main",
				Label:    "main (google/gemini-3.6-flash)",
				Provider: "openclaw",
			},
		}}, nil
	}
	defer func() { listModels = orig }()

	tierMap := buildOpenclawTierMap(context.Background(), agent.Command{})

	// gemini-3.6-flash → flash → TierSimple
	// So all three tiers should map to "main" (only one agent)
	for _, tier := range []cerebra.Tier{cerebra.TierSimple, cerebra.TierStandard, cerebra.TierHeavy} {
		if tierMap[tier] == "" {
			t.Errorf("tier %s is empty, want 'main'", tier)
		}
		if tierMap[tier] != "main" {
			t.Errorf("tier %s = %q, want 'main'", tier, tierMap[tier])
		}
	}
}

func TestBuildOpenclawTierMap_MultiAgent(t *testing.T) {
	// Hypothetical multi-agent setup with clear tier spread
	orig := listModels
	listModels = func(_ context.Context, provider string, _ agent.Command) (agent.Catalog, error) {
		return agent.Catalog{Models: []agent.Model{
			{ID: "fast-agent",  Label: "fast (google/gemini-2.5-flash)",        Provider: "openclaw"}, // flash → Simple
			{ID: "balanced",    Label: "balanced (google/gemini-2.5-sonnet)",   Provider: "openclaw"}, // sonnet → Standard
			{ID: "heavy-agent", Label: "heavy (google/gemini-ultra)",           Provider: "openclaw"}, // ultra → Heavy
		}}, nil
	}
	defer func() { listModels = orig }()

	tierMap := buildOpenclawTierMap(context.Background(), agent.Command{})

	if tierMap[cerebra.TierSimple] != "fast-agent" {
		t.Errorf("Simple = %q, want fast-agent", tierMap[cerebra.TierSimple])
	}
	if tierMap[cerebra.TierStandard] != "balanced" {
		t.Errorf("Standard = %q, want balanced", tierMap[cerebra.TierStandard])
	}
	if tierMap[cerebra.TierHeavy] != "heavy-agent" {
		t.Errorf("Heavy = %q, want heavy-agent", tierMap[cerebra.TierHeavy])
	}
}
