package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

func TestValidatePublishable(t *testing.T) {
	cases := []struct {
		name    string
		dsl     string
		wantErr bool
	}{
		{
			name:    "source-backed approved is publishable",
			dsl:     `{"source":{"source_system":"vaccinations_db","source_ref":"VaccDB ref","review_status":"approved","approved_by":"R. Teja","approved_at":"2026-06-26T00:00:00Z"}}`,
			wantErr: false,
		},
		{
			name:    "phc source approved is publishable",
			dsl:     `{"source":{"source_system":"phc","source_ref":"PHC §6","review_status":"approved","approved_by":"Reviewer","approved_at":"2026-06-26T00:00:00Z"}}`,
			wantErr: false,
		},
		{
			name:    "manual_admin is not publishable",
			dsl:     `{"source":{"source_system":"manual_admin","source_ref":"x","review_status":"approved","approved_by":"x","approved_at":"2026-06-26T00:00:00Z"}}`,
			wantErr: true,
		},
		{
			name:    "missing source_ref is not publishable",
			dsl:     `{"source":{"source_system":"vet","source_ref":"","review_status":"approved","approved_by":"x","approved_at":"2026-06-26T00:00:00Z"}}`,
			wantErr: true,
		},
		{
			name:    "not approved is not publishable",
			dsl:     `{"source":{"source_system":"vet","source_ref":"ref","review_status":"reviewed","approved_by":"x","approved_at":"2026-06-26T00:00:00Z"}}`,
			wantErr: true,
		},
		{
			name:    "missing approved_by is not publishable",
			dsl:     `{"source":{"source_system":"vet","source_ref":"ref","review_status":"approved","approved_by":"","approved_at":"2026-06-26T00:00:00Z"}}`,
			wantErr: true,
		},
		{
			name:    "missing approved_at is not publishable",
			dsl:     `{"source":{"source_system":"vet","source_ref":"ref","review_status":"approved","approved_by":"x","approved_at":""}}`,
			wantErr: true,
		},
		{
			name:    "invalid approved_at is not publishable",
			dsl:     `{"source":{"source_system":"vet","source_ref":"ref","review_status":"approved","approved_by":"x","approved_at":"on approve"}}`,
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

func TestValidateExecutionContract(t *testing.T) {
	valid := domain.Version{
		SopVersionID: "62000000-0000-4000-8000-000000000001",
		ProofPolicy:  []byte(`{"required":true,"types":["video"]}`),
		RuleDsl:      []byte(`{"schedule":[{"dose_code":"primary"}]}`),
	}
	if err := ValidateExecutionContract(valid); err != nil {
		t.Fatalf("valid execution contract rejected: %v", err)
	}

	missingSOP := valid
	missingSOP.SopVersionID = ""
	if err := ValidateExecutionContract(missingSOP); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("missing SOP should be not publishable, got %v", err)
	}

	missingProof := valid
	missingProof.ProofPolicy = []byte(`{}`)
	if err := ValidateExecutionContract(missingProof); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("missing proof policy should be not publishable, got %v", err)
	}

	// {"required_proofs":[]} is a non-empty JSON object but carries zero real proof requirements:
	// it must NOT pass the execution-contract gate (the old non-empty-object check let it through).
	emptyRequiredProofs := valid
	emptyRequiredProofs.ProofPolicy = []byte(`{"required_proofs":[]}`)
	if err := ValidateExecutionContract(emptyRequiredProofs); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("empty required_proofs should be not publishable, got %v", err)
	}

	// A real version-level proof requirement (object with a recognized array key) passes.
	realProof := valid
	realProof.ProofPolicy = []byte(`{"required_proofs":["administration_video"]}`)
	if err := ValidateExecutionContract(realProof); err != nil {
		t.Fatalf("non-empty required_proofs rejected: %v", err)
	}

	// Version-level proof_policy is OBJECT-shaped. Every value below must be rejected: a bare array
	// is the wrong shape at the version level, blank tokens are not requirements, and metadata-only
	// or scalar objects carry no proof array.
	for _, badProof := range []string{
		`["video"]`,                   // array shape is invalid at the version level
		`{"required_proofs":[""]}`,    // blank token
		`{"required_proofs":["   "]}`, // whitespace-only token
		`{"subject_scope":"batch"}`,   // metadata, no recognized proof array
		`{"required":true}`,           // recognized key but scalar, not an array of tokens
		`[]`,                          // empty array
	} {
		bad := valid
		bad.ProofPolicy = []byte(badProof)
		if err := ValidateExecutionContract(bad); !errors.Is(err, ErrNotPublishable) {
			t.Fatalf("version-level proof %q should be not publishable, got %v", badProof, err)
		}
	}

	// Row-level proof is exercised through rule_dsl schedule[].proof_policy, NOT by putting an array
	// in the version-level ProofPolicy. The version-level proof stays a valid object; the schedule
	// row carries its own proof.
	rowArrayProof := valid
	rowArrayProof.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary","proof_policy":["administration_video"]}]}`)
	if err := ValidateExecutionContract(rowArrayProof); err != nil {
		t.Fatalf("row-level proof array rejected: %v", err)
	}

	// A row that supplies a blank proof array is rejected — its own blank proof does not fall back to
	// the (valid) version-level proof.
	rowBlankProof := valid
	rowBlankProof.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary","proof_policy":[""]}]}`)
	if err := ValidateExecutionContract(rowBlankProof); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("row-level blank proof array should be not publishable, got %v", err)
	}

	// A row that OMITS proof_policy inherits the validated version-level proof and still publishes.
	rowOmittedProof := valid
	rowOmittedProof.RuleDsl = []byte(`{"schedule":[{"dose_code":"primary"}]}`)
	if err := ValidateExecutionContract(rowOmittedProof); err != nil {
		t.Fatalf("row omitting proof should inherit version proof, got %v", err)
	}
}

func TestPublishVersionAlreadyPublishedRetryIsNoop(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("published"),
	}
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if err != nil {
		t.Fatalf("publish already-published retry: %v", err)
	}
	if repo.publishCalled {
		t.Fatalf("repo publish must not run for already-published retry")
	}
}

func TestPublishVersionRejectsRetiredVersion(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("retired"),
	}
	service := NewService(repo)

	err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil)
	if !errors.Is(err, ports.ErrVersionNotDraft) {
		t.Fatalf("publish retired err=%v, want ErrVersionNotDraft", err)
	}
}

func TestPublishVersionDelegatesDraftPublishToRepository(t *testing.T) {
	repo := &fakeProtocolRepo{
		version: validPublishVersion("draft"),
	}
	service := NewService(repo)

	if err := service.PublishVersion(context.Background(), "tenant-1", "version-1", nil); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !repo.publishCalled {
		t.Fatalf("repo publish was not called")
	}
	if repo.version.Status != "published" {
		t.Fatalf("repo status = %s, want published", repo.version.Status)
	}
}

func validPublishVersion(status string) domain.Version {
	return domain.Version{
		ProtocolVersionID: "version-1",
		ProtocolID:        "protocol-1",
		Category:          "vaccination",
		Status:            status,
		SopVersionID:      "62000000-0000-4000-8000-000000000001",
		ProofPolicy:       []byte(`{"required_proofs":["administration_video"]}`),
		RuleDsl:           []byte(`{"source":{"source_system":"vaccinations_db","source_ref":"VaccDB ref","review_status":"approved","approved_by":"R. Teja","approved_at":"2026-06-26T00:00:00Z"},"schedule":[{"dose_code":"primary"}]}`),
	}
}

type fakeProtocolRepo struct {
	version       domain.Version
	publishCalled bool
	publishCalls  int
}

func (f *fakeProtocolRepo) Ping(context.Context) error { return nil }
func (f *fakeProtocolRepo) CreateDefinition(context.Context, domain.NewDefinition) (string, error) {
	return "", nil
}
func (f *fakeProtocolRepo) GetDefinitionByCode(context.Context, string, string) (domain.Definition, error) {
	return domain.Definition{}, nil
}
func (f *fakeProtocolRepo) CreateVersion(context.Context, domain.NewVersion) (string, error) {
	return "", nil
}
func (f *fakeProtocolRepo) GetVersion(context.Context, string, string) (domain.Version, error) {
	return f.version, nil
}
func (f *fakeProtocolRepo) ListPublishedVersions(context.Context, string, string) ([]domain.Version, error) {
	return nil, nil
}
func (f *fakeProtocolRepo) ListConfigs(context.Context, string, string) ([]domain.ConfigListItem, error) {
	return nil, nil
}
func (f *fakeProtocolRepo) PublishVersion(context.Context, string, string, *string) error {
	f.publishCalled = true
	f.publishCalls++
	f.version.Status = "published"
	return nil
}
func (f *fakeProtocolRepo) CreateRule(context.Context, domain.NewRule) (string, error) {
	return "", nil
}
func (f *fakeProtocolRepo) ListRules(context.Context, string, string) ([]domain.Rule, error) {
	return nil, nil
}
func (f *fakeProtocolRepo) ListActiveAnimalStages(context.Context, string) ([]domain.AnimalStage, error) {
	return nil, nil
}
func (f *fakeProtocolRepo) CreateTrigger(context.Context, domain.NewTrigger) (string, error) {
	return "", nil
}
