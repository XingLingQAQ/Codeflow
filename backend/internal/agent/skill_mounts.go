package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/codeflow/backend/internal/skill"
)

// RenderInjectionForAgent returns the skill injection block scoped to an agent's
// mounted skills. The filtering rules are:
//
//   - Unknown agent ID returns an error.
//   - Disabled agent returns an empty string (no injection).
//   - Empty Mounts.Skills means unrestricted: all matching skills are eligible
//     (equivalent to calling skill.Registry.RenderInjection directly). The five
//     builtin agents intentionally leave Mounts.Skills empty so they inherit the
//     full skill surface.
//   - Non-empty Mounts.Skills restricts the result to skills whose ID or Name
//     appears in the mount list (case-insensitive name match for ergonomics;
//     exact ID match). Only skills that also pass the normal Match criteria
//     (trigger/stage) are included.
func RenderInjectionForAgent(
	ctx context.Context,
	reg AgentRegistry,
	skills skill.Registry,
	agentID string,
	req *skill.MatchRequest,
) (string, error) {
	a, err := reg.Get(ctx, agentID)
	if err != nil {
		return "", fmt.Errorf("load agent for injection: %w", err)
	}
	if !a.Enabled {
		return "", nil
	}

	if len(a.Mounts.Skills) == 0 {
		return skills.RenderInjection(ctx, req)
	}

	matches, err := skills.Match(ctx, req)
	if err != nil {
		return "", err
	}

	mountSet := buildMountSet(a.Mounts.Skills)

	var b strings.Builder
	count := 0
	for _, m := range matches {
		if !isMounted(mountSet, m.Skill.ID, m.Skill.Name) {
			continue
		}
		if count == 0 {
			b.WriteString("## Active Skills\n")
		}
		b.WriteString("\n### ")
		b.WriteString(m.Skill.Name)
		b.WriteString(" (")
		b.WriteString(m.Skill.Version)
		b.WriteString(")\n")
		b.WriteString(m.Skill.Body)
		b.WriteString("\n")
		count++
	}
	return b.String(), nil
}

type mountEntry struct {
	id        string
	nameLower string
}

func buildMountSet(mounts []string) []mountEntry {
	out := make([]mountEntry, len(mounts))
	for i, m := range mounts {
		out[i] = mountEntry{id: m, nameLower: strings.ToLower(m)}
	}
	return out
}

func isMounted(set []mountEntry, id, name string) bool {
	nameLower := strings.ToLower(name)
	for _, e := range set {
		if e.id == id {
			return true
		}
		if e.nameLower == nameLower {
			return true
		}
	}
	return false
}
