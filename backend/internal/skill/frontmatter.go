package skill

import (
	"strings"
)

// parseFrontmatter extracts YAML-like key: value pairs from a leading --- block.
// Only flat string/list fields used by Skill are recognized; unknown keys ignored.
// Returns remaining body (without frontmatter) and partial CreateRequest overlays.
func parseFrontmatter(raw string) (body string, overlay CreateRequest, ok bool) {
	// strip UTF-8 BOM if present
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		raw = raw[3:]
	}
	if !strings.HasPrefix(raw, "---") {
		return raw, CreateRequest{}, false
	}
	rest := strings.TrimPrefix(raw, "---")
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return raw, CreateRequest{}, false
	}
	fm := rest[:end]
	body = strings.TrimLeft(rest[end+len("\n---"):], "\r\n")
	overlay = CreateRequest{}
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+1:])
		val = strings.Trim(val, `"'`)
		switch strings.ToLower(key) {
		case "name":
			overlay.Name = val
		case "description":
			overlay.Description = val
		case "version":
			overlay.Version = val
		case "triggers":
			overlay.Triggers = splitCSV(val)
		case "stage_tags", "stage-tags", "stages":
			overlay.StageTags = splitCSV(val)
		case "source":
			overlay.Source = Source(val)
		}
	}
	return body, overlay, true
}

func splitCSV(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// applyFrontmatter fills empty CreateRequest fields from frontmatter in Body.
func applyFrontmatter(req *CreateRequest) {
	if req == nil || req.Body == "" {
		return
	}
	body, overlay, ok := parseFrontmatter(req.Body)
	if !ok {
		return
	}
	req.Body = body
	if req.Name == "" && overlay.Name != "" {
		req.Name = overlay.Name
	}
	if req.Description == "" && overlay.Description != "" {
		req.Description = overlay.Description
	}
	if req.Version == "" && overlay.Version != "" {
		req.Version = overlay.Version
	}
	if len(req.Triggers) == 0 && len(overlay.Triggers) > 0 {
		req.Triggers = overlay.Triggers
	}
	if len(req.StageTags) == 0 && len(overlay.StageTags) > 0 {
		req.StageTags = overlay.StageTags
	}
	if req.Source == "" && overlay.Source != "" {
		req.Source = overlay.Source
	}
}
