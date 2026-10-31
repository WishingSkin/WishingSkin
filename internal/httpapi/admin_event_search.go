package httpapi

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
)

//go:embed admin_event_labels.json
var adminEventLabelsJSON []byte

// Shared with the management UI so searches follow the displayed action names.
var adminEventLabels = func() map[string]map[string]map[string]string {
	var labels map[string]map[string]map[string]string
	if err := json.Unmarshal(adminEventLabelsJSON, &labels); err != nil {
		panic(err)
	}
	return labels
}()

func adminEventLabelMatches(group, query string) []string {
	var matches []string
	for key, labels := range adminEventLabels[group] {
		for _, label := range labels {
			if strings.Contains(strings.ToLower(label), query) {
				matches = append(matches, key)
				break
			}
		}
	}
	sort.Strings(matches)
	return matches
}
