// Package app compiles the mobile remote-config ("live-config bundle") use-case.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/appconfig/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// Config is the backend-bounded, env-configurable ClientRuntimeConfig defaults. Every
// field is clamped to a safe [min,max] range in ConfigFromEnv — the client never
// receives an unbounded operational knob (docs/mobile/backend-driven-config.md:
// "backend-bounded ... the server clamps each to a safe min/max").
type Config struct {
	PageSizeDefault   int
	SyncBackoffBaseMs int
	SyncBackoffMaxMs  int
	RefreshCadenceSec int
	CacheTTLSec       int
	JankSamplingRate  float64
}

const (
	defaultPageSizeDefault = 50
	minPageSizeDefault     = 10
	maxPageSizeDefault     = 200

	defaultSyncBackoffBaseMs = 1000
	minSyncBackoffBaseMs     = 250
	maxSyncBackoffBaseMs     = 30000

	defaultSyncBackoffMaxMs = 60000
	minSyncBackoffMaxMs     = 1000
	maxSyncBackoffMaxMs     = 300000

	defaultRefreshCadenceSec = 300
	minRefreshCadenceSec     = 30
	maxRefreshCadenceSec     = 3600

	defaultCacheTTLSec = 60
	minCacheTTLSec     = 5
	maxCacheTTLSec     = 3600

	defaultJankSamplingRate = 0.05
	minJankSamplingRate     = 0
	maxJankSamplingRate     = 1
)

// ConfigFromEnv reads GOATOS_APP_CONFIG_* overrides (falling back to safe defaults)
// and clamps every value to its bounded range.
func ConfigFromEnv() Config {
	return Config{
		PageSizeDefault:   clampInt(envInt("GOATOS_APP_CONFIG_PAGE_SIZE_DEFAULT", defaultPageSizeDefault), minPageSizeDefault, maxPageSizeDefault),
		SyncBackoffBaseMs: clampInt(envInt("GOATOS_APP_CONFIG_SYNC_BACKOFF_BASE_MS", defaultSyncBackoffBaseMs), minSyncBackoffBaseMs, maxSyncBackoffBaseMs),
		SyncBackoffMaxMs:  clampInt(envInt("GOATOS_APP_CONFIG_SYNC_BACKOFF_MAX_MS", defaultSyncBackoffMaxMs), minSyncBackoffMaxMs, maxSyncBackoffMaxMs),
		RefreshCadenceSec: clampInt(envInt("GOATOS_APP_CONFIG_REFRESH_CADENCE_SEC", defaultRefreshCadenceSec), minRefreshCadenceSec, maxRefreshCadenceSec),
		CacheTTLSec:       clampInt(envInt("GOATOS_APP_CONFIG_CACHE_TTL_SEC", defaultCacheTTLSec), minCacheTTLSec, maxCacheTTLSec),
		JankSamplingRate:  clampFloat(envFloat("GOATOS_APP_CONFIG_JANK_SAMPLING_RATE", defaultJankSamplingRate), minJankSamplingRate, maxJankSamplingRate),
	}
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func envFloat(key string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return v
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// ModuleOwnershipReader resolves the actor's owned product modules from the same
// real, department-sourced read /app/bootstrap and /admin-web/bootstrap already use
// (backend/internal/permissions/adapters/postgres.ModuleOwnershipSource). Depending
// on the permissions package's own interface here keeps this package out of Postgres
// directly and out of the workforce/adminui module boundaries.
type ModuleOwnershipReader = permissions.ModuleOwnershipSource

// Input identifies the actor whose owned-module set this compile call resolves.
type Input struct {
	TenantID string
	ActorID  string
}

// Service compiles the mobile remote-config bundle.
type Service struct {
	ownership ModuleOwnershipReader
	runtime   Config
	flags     map[string]bool
}

// NewService constructs the config-compile service with the curated, bounded
// feature-flag registry (defaultFeatureFlags).
func NewService(ownership ModuleOwnershipReader, runtime Config) *Service {
	return &Service{ownership: ownership, runtime: runtime, flags: defaultFeatureFlags()}
}

// defaultFeatureFlags is the initial curated mobile feature-flag registry: real
// capabilities live behind this build (the vaccination gaps + coverage overlays this
// change ships). A future tenant/role-scoped flag store can replace this static map
// without changing the response contract.
func defaultFeatureFlags() map[string]bool {
	return map[string]bool{
		"vaccination_gaps_overlay":     true,
		"vaccination_coverage_overlay": true,
	}
}

// hashInput is the subset of the response that participates in the revision/ETag
// hash — everything the client can observe changing except the hash-derived fields
// themselves (Revision, CachePolicy).
type hashInput struct {
	FeatureFlags        map[string]bool            `json:"featureFlags"`
	OwnedModules        []permissions.OwnedModule  `json:"ownedModules"`
	ClientRuntimeConfig domain.ClientRuntimeConfig `json:"clientRuntimeConfig"`
	PolicyRevision      string                     `json:"policyRevision"`
}

// Compile resolves the actor's owned modules and assembles the config bundle,
// stamping a content-hash revision + ETag (mirrors the adminui bootstrap compile
// pattern: hash the canonical payload, format `W/"<hash>"`, no DB write involved).
func (s *Service) Compile(ctx context.Context, in Input) (domain.Response, error) {
	owned, err := s.ownedModules(ctx, in)
	if err != nil {
		return domain.Response{}, err
	}
	runtimeConfig := domain.ClientRuntimeConfig{
		PageSizeDefault:   s.runtime.PageSizeDefault,
		SyncBackoffBaseMs: s.runtime.SyncBackoffBaseMs,
		SyncBackoffMaxMs:  s.runtime.SyncBackoffMaxMs,
		RefreshCadenceSec: s.runtime.RefreshCadenceSec,
		CacheTTLSec:       s.runtime.CacheTTLSec,
		JankSamplingRate:  s.runtime.JankSamplingRate,
	}
	policyRevision := "docs/preventive-care-vaccination/vaccination-rules.md"

	revision := hashRevision(hashInput{
		FeatureFlags:        s.flags,
		OwnedModules:        owned,
		ClientRuntimeConfig: runtimeConfig,
		PolicyRevision:      policyRevision,
	})

	return domain.Response{
		Source:       domain.SourceAPI,
		Revision:     revision,
		FeatureFlags: s.flags,
		OwnedModules: owned,
		CachePolicy: domain.CachePolicy{
			ETag:            `W/"` + revision + `"`,
			InProcessTTLSec: s.runtime.CacheTTLSec,
			RedisTTLHintSec: s.runtime.CacheTTLSec * 10,
			RevisionSource:  "feature-flags+owned-modules+client-runtime-config",
		},
		ClientRuntimeConfig: runtimeConfig,
		PolicyRevision:      policyRevision,
	}, nil
}

func (s *Service) ownedModules(ctx context.Context, in Input) ([]permissions.OwnedModule, error) {
	if s.ownership == nil || strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ActorID) == "" {
		return []permissions.OwnedModule{}, nil
	}
	owned, err := s.ownership.ListActiveModuleGrantsForActor(ctx, in.ActorID, in.TenantID)
	if err != nil {
		return nil, err
	}
	if owned == nil {
		owned = []permissions.OwnedModule{}
	}
	return owned, nil
}

func hashRevision(in hashInput) string {
	b, err := json.Marshal(in)
	if err != nil {
		return hashString(err.Error())
	}
	return hashString(string(b))
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}
