package localization

import (
	"strconv"
	"strings"
)

const DefaultTag = "en"

var supportedTags = map[string]struct{}{
	"en": {},
	"hi": {},
	"kn": {},
	"te": {},
}

// SupportedTags returns the app locales currently supported by Goat OS.
func SupportedTags() []string {
	return []string{"en", "hi", "kn", "te"}
}

// Normalize returns a supported Goat OS locale tag, falling back to English.
func Normalize(tag string) string {
	if normalized, ok := normalizeCandidate(tag); ok {
		return normalized
	}
	return DefaultTag
}

// FromHeaders resolves the app-specific locale header first, then the standard
// Accept-Language list. The app-specific header is the exact selected language;
// Accept-Language remains useful for generic clients and proxies.
func FromHeaders(localeHeader, acceptLanguage string) string {
	if normalized, ok := normalizeCandidate(localeHeader); ok {
		return normalized
	}
	return FromAcceptLanguage(acceptLanguage)
}

// FromAcceptLanguage picks the best supported tag from an Accept-Language value.
func FromAcceptLanguage(header string) string {
	bestTag := ""
	bestQ := -1.0
	for _, rawPart := range strings.Split(header, ",") {
		part := strings.TrimSpace(rawPart)
		if part == "" {
			continue
		}
		tag := part
		q := 1.0
		if before, after, found := strings.Cut(part, ";"); found {
			tag = strings.TrimSpace(before)
			for _, param := range strings.Split(after, ";") {
				key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
				if ok && strings.EqualFold(strings.TrimSpace(key), "q") {
					if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
						q = parsed
					}
				}
			}
		}
		if q <= 0 {
			continue
		}
		if normalized, ok := normalizeCandidate(tag); ok && q > bestQ {
			bestTag = normalized
			bestQ = q
		}
	}
	if bestTag != "" {
		return bestTag
	}
	return DefaultTag
}

func normalizeCandidate(tag string) (string, bool) {
	tag = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"))
	if tag == "" || strings.ContainsAny(tag, " \t\r\n") {
		return "", false
	}
	if _, ok := supportedTags[tag]; ok {
		return tag, true
	}
	if base, _, found := strings.Cut(tag, "-"); found {
		if _, ok := supportedTags[base]; ok {
			return base, true
		}
	}
	return "", false
}
