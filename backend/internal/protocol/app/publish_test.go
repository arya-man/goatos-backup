package app

import (
	"errors"
	"testing"
)

func TestValidatePublishable(t *testing.T) {
	cases := []struct {
		name    string
		dsl     string
		wantErr bool
	}{
		{
			name:    "source-backed approved is publishable",
			dsl:     `{"source":{"source_system":"vaccinations_db","source_ref":"VaccDB ref","review_status":"approved","approved_by":"R. Teja"}}`,
			wantErr: false,
		},
		{
			name:    "phc source approved is publishable",
			dsl:     `{"source":{"source_system":"phc","source_ref":"PHC §6","review_status":"approved","approved_by":"Reviewer"}}`,
			wantErr: false,
		},
		{
			name:    "manual_admin is not publishable",
			dsl:     `{"source":{"source_system":"manual_admin","source_ref":"x","review_status":"approved","approved_by":"x"}}`,
			wantErr: true,
		},
		{
			name:    "missing source_ref is not publishable",
			dsl:     `{"source":{"source_system":"vet","source_ref":"","review_status":"approved","approved_by":"x"}}`,
			wantErr: true,
		},
		{
			name:    "not approved is not publishable",
			dsl:     `{"source":{"source_system":"vet","source_ref":"ref","review_status":"reviewed","approved_by":"x"}}`,
			wantErr: true,
		},
		{
			name:    "missing approved_by is not publishable",
			dsl:     `{"source":{"source_system":"vet","source_ref":"ref","review_status":"approved","approved_by":""}}`,
			wantErr: true,
		},
		{
			name:    "empty dsl is not publishable",
			dsl:     ``,
			wantErr: true,
		},
		{
			name:    "no source object is not publishable",
			dsl:     `{"category":"vaccination"}`,
			wantErr: true,
		},
		{
			name:    "invalid json is not publishable",
			dsl:     `{not json`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePublishable([]byte(tc.dsl))
			if tc.wantErr && err == nil {
				t.Fatalf("expected not-publishable, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected publishable, got %v", err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrNotPublishable) {
				t.Fatalf("expected ErrNotPublishable, got %v", err)
			}
		})
	}
}
