package domain

import (
	"regexp"
	"strings"
)

var (
	drivePartPattern   = regexp.MustCompile(`(?i)^(.+?)\s*-\s*(part\s+\d+)$`)
	driveNumberPattern = regexp.MustCompile(`^(.+?)\s+(\d+)$`)
)

// NormalizeDriveShed splits source labels like "Gandhi 1" and "Godel 1 - Part 3"
// into physical shed and partition labels used by drive planning/read models.
func NormalizeDriveShed(raw string) (physicalShed, partition string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	raw = strings.Join(strings.Fields(raw), " ")
	if matches := drivePartPattern.FindStringSubmatch(raw); len(matches) == 3 {
		return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2])
	}
	if matches := driveNumberPattern.FindStringSubmatch(raw); len(matches) == 3 {
		return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2])
	}
	return raw, "whole"
}
