package app

import (
	"fmt"
	"strings"
)

// MaxVaccinesPerComboSession is the maximum distinct vaccine products allowed on one shed visit combo.
const MaxVaccinesPerComboSession = 3

// comboSessionID builds a deterministic combo session key from at most MaxVaccinesPerComboSession members.
func comboSessionID(members []string) (string, error) {
	normalized := normalizeComboMembers(members)
	if len(normalized) == 0 {
		return "", fmt.Errorf("obligation: combo session requires at least one vaccine")
	}
	if len(normalized) > MaxVaccinesPerComboSession {
		return "", fmt.Errorf("obligation: combo session %v exceeds max %d vaccines per visit", normalized, MaxVaccinesPerComboSession)
	}
	return "combo:" + strings.Join(normalized, "+"), nil
}

func normalizeComboMembers(members []string) []string {
	seen := make(map[string]struct{}, len(members))
	out := make([]string, 0, len(members))
	for _, member := range members {
		code := strings.TrimSpace(member)
		if code == "" {
			continue
		}
		key := strings.ToLower(code)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, code)
	}
	return out
}

// splitComboMembers chunks vaccine codes into same-day bundles of at most MaxVaccinesPerComboSession.
func splitComboMembers(members []string) [][]string {
	normalized := normalizeComboMembers(members)
	if len(normalized) == 0 {
		return nil
	}
	if len(normalized) <= MaxVaccinesPerComboSession {
		return [][]string{normalized}
	}
	chunks := make([][]string, 0, (len(normalized)+MaxVaccinesPerComboSession-1)/MaxVaccinesPerComboSession)
	for start := 0; start < len(normalized); start += MaxVaccinesPerComboSession {
		end := start + MaxVaccinesPerComboSession
		if end > len(normalized) {
			end = len(normalized)
		}
		chunks = append(chunks, normalized[start:end])
	}
	return chunks
}

func validateApprovedDriveCombos() error {
	for label, members := range approvedDriveCombos {
		if len(normalizeComboMembers(members)) > MaxVaccinesPerComboSession {
			return fmt.Errorf("approved combo %q has more than %d vaccines", label, MaxVaccinesPerComboSession)
		}
		if _, err := comboSessionID(members); err != nil {
			return fmt.Errorf("approved combo %q: %w", label, err)
		}
	}
	return nil
}
