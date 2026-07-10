// Package domain holds the mobile remote-config ("live-config bundle") read model.
//
// This backs docs/mobile/backend-driven-config.md: the app is a dumb renderer, and
// presentationConfig (nav/labels/flags/module kill-switch) + clientRuntimeConfig
// (bounded operational knobs) are pushed on a monotonic revision + ETag so a config
// change lands on the next poll without a new APK. Business/medical policy is
// deliberately NOT modeled here — only a read-only policy_revision echo travels to
// the client, per the same doc.
package domain

// SourceAPI marks every response as backend-computed (matches the `source: api`
// convention used by the other app-tier read models in this repo).
const SourceAPI = "api"

// CachePolicy is the conditional-GET contract for the config bundle: an ETag for
// If-None-Match/304, an in-process cache TTL, a Redis TTL hint for a future shared
// response cache, and a label naming what the revision was derived from. Same shape
// as adminui's ContractCachePolicy (contracts/openapi/app-api.yaml
// AdminWebContractCachePolicy) — kept as an independent Go type per module boundary,
// while the OpenAPI contract reuses the same component schema.
type CachePolicy struct {
	ETag            string `json:"etag"`
	InProcessTTLSec int    `json:"inProcessTtlSec"`
	RedisTTLHintSec int    `json:"redisTtlHintSec"`
	RevisionSource  string `json:"revisionSource"`
}

// ClientRuntimeConfig is backend-owned, backend-bounded operational tuning — never a
// business/medical policy value — per docs/mobile/backend-driven-config.md: default
// page size, sync backoff bounds, refresh cadence, cache TTL, and jank-sampling rate.
// Every field is clamped server-side to a safe min/max before it ever reaches this
// struct (see app.ConfigFromEnv).
type ClientRuntimeConfig struct {
	PageSizeDefault   int     `json:"pageSizeDefault"`
	SyncBackoffBaseMs int     `json:"syncBackoffBaseMs"`
	SyncBackoffMaxMs  int     `json:"syncBackoffMaxMs"`
	RefreshCadenceSec int     `json:"refreshCadenceSec"`
	CacheTTLSec       int     `json:"cacheTtlSec"`
	JankSamplingRate  float64 `json:"jankSamplingRate"`
}

// Response is the full /app/config payload. FeatureFlags are the presentation
// config slice this endpoint owns; PolicyRevision is a read-only traceability
// echo (the governing business-rule doc name), never a raw threshold the client
// could compute from.
type Response struct {
	Source              string              `json:"source"`
	Revision            string              `json:"revision"`
	CachePolicy         CachePolicy         `json:"cachePolicy"`
	FeatureFlags        map[string]bool     `json:"featureFlags"`
	ClientRuntimeConfig ClientRuntimeConfig `json:"clientRuntimeConfig"`
	PolicyRevision      string              `json:"policyRevision"`
}
