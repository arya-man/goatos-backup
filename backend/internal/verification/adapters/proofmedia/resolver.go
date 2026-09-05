// Package proofmedia adapts the EXISTING proof module's signed-URL download port to Verification's
// ports.MediaResolver — verification never proxies or duplicates media bytes, it only resolves
// streamed, signed download URLs at read time (verification-module-design.md §2.5).
package proofmedia

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	vports "github.com/vgoats/goatos/backend/internal/verification/ports"
)

const mediaCacheTTL = 5 * time.Minute

// Downloader is the minimal slice of proof/app.Service this adapter needs. proofapp.Service already
// satisfies this signature (DownloadURL(ctx, tenantID, proofID) (string, error)).
type Downloader interface {
	DownloadURL(ctx context.Context, tenantID, proofID string) (string, error)
}

type ArtifactDownloader interface {
	DownloadArtifact(ctx context.Context, tenantID, proofID string) (proofdomain.Artifact, string, error)
}

// ObjectAvailabilityChecker is the slice of proof/app.Service that proves a stored object still
// exists. proofapp.Service satisfies it via EnsureObjectAvailable.
type ObjectAvailabilityChecker interface {
	EnsureObjectAvailable(ctx context.Context, tenantID, proofID string) error
}

// ActionPresentationResolver resolves backend-authored task titles and recorded operator answers
// referenced by proof metadata.
type ActionPresentationResolver interface {
	ResolveActionPresentations(ctx context.Context, tenantID string, actionIDs []string) (map[string]string, map[string]string, error)
}

type Resolver struct {
	proof               Downloader
	actionPresentations ActionPresentationResolver
	cacheMu             sync.Mutex
	cache               map[string]mediaCacheEntry
}

type mediaCacheEntry struct {
	expiresAt time.Time
	item      domain.MediaItem
	actionID  string
}

func NewResolver(proof Downloader) *Resolver {
	return &Resolver{proof: proof, cache: map[string]mediaCacheEntry{}}
}

func (r *Resolver) WithActionPresentationResolver(presentations ActionPresentationResolver) *Resolver {
	r.actionPresentations = presentations
	return r
}

func (r *Resolver) cachedMedia(tenantID, proofID string) (domain.MediaItem, string, bool) {
	key := tenantID + ":" + proofID
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	entry, ok := r.cache[key]
	if !ok {
		return domain.MediaItem{}, "", false
	}
	if time.Now().After(entry.expiresAt) {
		delete(r.cache, key)
		return domain.MediaItem{}, "", false
	}
	return entry.item, entry.actionID, true
}

func (r *Resolver) setCachedMedia(tenantID, proofID string, item domain.MediaItem, actionID string) {
	if item.DownloadURL == "" {
		return
	}
	item.Answer = ""
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	r.cache[tenantID+":"+proofID] = mediaCacheEntry{expiresAt: time.Now().Add(mediaCacheTTL), item: item, actionID: actionID}
}

// ResolveMedia resolves every requested proof and reports per-ID failures. A missing or unsignable
// individual proof is reported as an empty entry for that ID with DownloadURL="", allowing items
// whose own refs all resolved to keep their media and evidence_available=true while items with
// unresolvable refs get marked evidence_available=false. The verifier UI and verdict path use this
// per-ID granularity to fail-closed only on the items that actually have missing evidence.
func (r *Resolver) ResolveMedia(ctx context.Context, tenantID string, proofIDs []string) ([]domain.MediaItem, error) {
	if r == nil || r.proof == nil {
		return nil, fmt.Errorf("verification proof resolver is unavailable")
	}
	out := make([]domain.MediaItem, len(proofIDs)) // Fixed-size array preserves ID order
	actionIDForProof := make([]string, len(proofIDs))

	rich, hasArtifactDownloader := r.proof.(ArtifactDownloader)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)

	for i, id := range proofIDs {
		out[i].ProofID = id // Pre-populate every slot with its ID for fail-closed detection
		if id == "" {
			continue // Empty ID stays as empty MediaItem; callers check DownloadURL=""
		}
		if cached, actionID, ok := r.cachedMedia(tenantID, id); ok {
			out[i] = cached
			actionIDForProof[i] = actionID
			continue
		}
		i, id := i, id
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if hasArtifactDownloader {
				proof, url, err := rich.DownloadArtifact(ctx, tenantID, id)
				if err != nil {
					return // Per-ID failure: leave out[i] with DownloadURL="" (zero value)
				}
				verificationLabel, _ := proof.Metadata["verification_label"].(string)
				actionID := ""
				if actionID, ok := proof.Metadata["action_id"].(string); ok && actionID != "" {
					actionIDForProof[i] = actionID
				}
				out[i] = domain.MediaItem{ProofID: id, DownloadURL: url, MimeType: proof.MimeType, DurationMS: proof.DurationMS, Label: verificationLabel}
				r.setCachedMedia(tenantID, id, out[i], actionID)
				return
			}
			url, err := r.proof.DownloadURL(ctx, tenantID, id)
			if err != nil {
				return // Per-ID failure: leave out[i] with DownloadURL="" (zero value)
			}
			out[i] = domain.MediaItem{ProofID: id, DownloadURL: url}
			r.setCachedMedia(tenantID, id, out[i], "")
		}()
	}
	wg.Wait()
	actionIDByProof := make(map[string]string, len(proofIDs))
	actionIDs := make([]string, 0, len(proofIDs))
	seenActionIDs := make(map[string]struct{}, len(proofIDs))
	for i := range out {
		actionID := actionIDForProof[i]
		if out[i].DownloadURL == "" || actionID == "" {
			continue
		}
		actionIDByProof[out[i].ProofID] = actionID
		if _, seen := seenActionIDs[actionID]; !seen {
			seenActionIDs[actionID] = struct{}{}
			actionIDs = append(actionIDs, actionID)
		}
	}
	if r.actionPresentations != nil && len(actionIDs) > 0 {
		labels, answers, err := r.actionPresentations.ResolveActionPresentations(ctx, tenantID, actionIDs)
		if err != nil {
			// Action presentation fetch failure doesn't unset previously resolved media URLs;
			// labels and answers simply stay empty for those items.
		} else {
			for i := range out {
				if out[i].DownloadURL == "" {
					continue // Skip unresolved items
				}
				actionID, ok := actionIDByProof[out[i].ProofID]
				if !ok {
					continue
				}
				label := labels[actionID]
				// Missing label for a resolved action is non-fatal; out[i].Label stays empty
				out[i].Label = label
				out[i].Answer = answers[actionID]
			}
		}
	}
	return out, nil
}

// EnsureEvidenceAvailable stats each of this ONE item's proof objects (verification/ports.
// EvidenceAvailabilityChecker). It is called only from RecordVerdict on an approve — never from the
// queue read, where one stat per proof per row would be an N+1 on a hot operator page. Do not
// "optimise" this back into ResolveMedia: signing a URL proves nothing about the bytes, which is
// exactly how an approve could previously be recorded against evidence that no longer existed.
func (r *Resolver) EnsureEvidenceAvailable(ctx context.Context, tenantID string, proofIDs []string) error {
	if r == nil || r.proof == nil {
		return fmt.Errorf("verification proof resolver is unavailable")
	}
	checker, ok := r.proof.(ObjectAvailabilityChecker)
	if !ok {
		return fmt.Errorf("verification proof storage cannot confirm evidence availability")
	}
	if len(proofIDs) == 0 {
		return vports.ErrEvidenceMissing
	}
	for _, id := range proofIDs {
		if strings.TrimSpace(id) == "" {
			return vports.ErrEvidenceMissing
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	sem := make(chan struct{}, 16)
	for _, id := range proofIDs {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var err error
			switch availabilityErr := checker.EnsureObjectAvailable(ctx, tenantID, id); {
			case availabilityErr == nil:
				return
			case errors.Is(availabilityErr, proofports.ErrObjectMissing), errors.Is(availabilityErr, proofports.ErrNotFound):
				err = fmt.Errorf("%w: proof %s", vports.ErrEvidenceMissing, id)
			default:
				err = fmt.Errorf("confirm verification proof %s: %w", id, availabilityErr)
			}
			mu.Lock()
			if firstErr == nil {
				firstErr = err
				cancel()
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return firstErr
}
