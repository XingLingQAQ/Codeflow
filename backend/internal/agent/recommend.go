package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// stageNaturalRole maps each floweng stage type to its natural primary role.
var stageNaturalRole = map[string]RoleBase{
	"coding":        RoleBaseCoder,
	"review":        RoleBaseCritic,
	"research":      RoleBaseResearcher,
	"idea":          RoleBaseMain,
	"design":        RoleBaseMain,
	"planning":      RoleBaseMain,
	"submit":        RoleBaseMain,
	"import":        RoleBaseResearcher,
	"comprehension": RoleBaseResearcher,
}

// SelectForStage returns up to limit agents ranked by suitability for stage.
//
// Ranking tiers (lower tier = higher rank):
//
//	Tier 0: stage tag matched + role fits the stage's natural role
//	Tier 1: stage tag matched + role does not fit
//	Tier 2: generalist (no stage tags) + role fits
//	Tier 3: generalist (no stage tags) + role does not fit
//
// Within a tier, tiebreak is Stats.Score desc, Stats.UsageCount desc, Name asc.
// Only enabled agents are considered. Agents whose StageTags are non-empty but
// do not contain stage are excluded. limit <= 0 defaults to 5.
func SelectForStage(ctx context.Context, reg AgentRegistry, stage string, limit int) ([]AgentAsset, error) {
	stage = strings.ToLower(strings.TrimSpace(stage))
	if stage == "" {
		return nil, fmt.Errorf("stage is required")
	}
	if limit <= 0 {
		limit = 5
	}

	candidates, err := reg.ListFiltered(ctx, stage, "", "")
	if err != nil {
		return nil, err
	}

	naturalRole := stageNaturalRole[stage]

	type ranked struct {
		asset AgentAsset
		tier  int
	}

	var items []ranked
	for _, a := range candidates {
		if !a.Enabled {
			continue
		}
		hasStageTag := false
		for _, t := range a.StageTags {
			if strings.ToLower(t) == stage {
				hasStageTag = true
				break
			}
		}
		hasRoleFit := naturalRole != "" && a.RoleBase == naturalRole

		tier := 3
		if hasStageTag && hasRoleFit {
			tier = 0
		} else if hasStageTag {
			tier = 1
		} else if hasRoleFit {
			tier = 2
		}

		items = append(items, ranked{asset: *a, tier: tier})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].tier != items[j].tier {
			return items[i].tier < items[j].tier
		}
		if items[i].asset.Stats.Score != items[j].asset.Stats.Score {
			return items[i].asset.Stats.Score > items[j].asset.Stats.Score
		}
		if items[i].asset.Stats.UsageCount != items[j].asset.Stats.UsageCount {
			return items[i].asset.Stats.UsageCount > items[j].asset.Stats.UsageCount
		}
		return items[i].asset.Name < items[j].asset.Name
	})

	if len(items) > limit {
		items = items[:limit]
	}

	out := make([]AgentAsset, len(items))
	for i := range items {
		out[i] = items[i].asset
	}
	return out, nil
}
