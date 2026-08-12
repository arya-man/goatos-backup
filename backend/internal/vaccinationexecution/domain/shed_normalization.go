package domain

import "strings"

// NormalizeDriveShed returns the exact shed name. Older code split names such as
// "Gandhi 1" into shed "Gandhi" + partition "1"; that is no longer valid because
// "Gandhi 1" is itself the shed.
func NormalizeDriveShed(raw string) (physicalShed, partition string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	raw = strings.Join(strings.Fields(raw), " ")
	return raw, "whole"
}
