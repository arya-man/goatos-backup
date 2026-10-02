package domain

import "testing"

func TestDisplayMappedByNamesSystemActorsInFarmWords(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		raw  *string
		want *string
	}{
		{nil, nil},
		{str("  "), nil},
		{str("migration:000298_smart_ble_tag_mapping_seed"), str(SystemMappedBy)},
		{str("seed-stg"), str(SystemMappedBy)},
		{str("system"), str(SystemMappedBy)},
		{str("maintainer"), str(SystemMappedBy)},
		{str("3b6f0f3e-9c1d-4c9a-9f0e-2a1b3c4d5e6f"), str("3b6f0f3e-9c1d-4c9a-9f0e-2a1b3c4d5e6f")},
	}
	for _, c := range cases {
		got := DisplayMappedBy(c.raw)
		switch {
		case c.want == nil && got != nil:
			t.Errorf("DisplayMappedBy(%v) = %q, want nil", c.raw, *got)
		case c.want != nil && (got == nil || *got != *c.want):
			t.Errorf("DisplayMappedBy(%q) = %v, want %q", *c.raw, got, *c.want)
		}
	}
}
