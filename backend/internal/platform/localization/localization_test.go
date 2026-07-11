package localization

import "testing"

func TestFromHeadersPrefersExplicitLocaleHeader(t *testing.T) {
	got := FromHeaders("te", "hi;q=1")
	if got != "te" {
		t.Fatalf("FromHeaders()=%q want te", got)
	}
}

func TestFromAcceptLanguageChoosesSupportedWeightedTag(t *testing.T) {
	got := FromAcceptLanguage("fr-FR, kn-IN;q=0.9, hi;q=0.8")
	if got != "kn" {
		t.Fatalf("FromAcceptLanguage()=%q want kn", got)
	}
}

func TestNormalizeFallsBackToEnglish(t *testing.T) {
	got := Normalize("fr-FR")
	if got != DefaultTag {
		t.Fatalf("Normalize()=%q want %q", got, DefaultTag)
	}
}
