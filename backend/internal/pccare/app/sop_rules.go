package app

import (
	"context"
	"errors"
	"strconv"
	"sync"

	fwrdomain "github.com/vgoats/goatos/backend/internal/feedwaterremoval/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// PC CARE SOP rule resolution (maintainer decision 2026-09-22, domain/sop.go).
//
// Two questions, asked at two moments:
//
//	publishedRules -- what a task planned NOW is stamped with (create, planner catalog);
//	rulesForTask   -- what an EXISTING task runs under (capture, submit, the removal card).
//
// Both fail open to the SEEDED rules when nothing is wired: the seeded document is the
// pre-SOP behaviour, so a test fake or a process without the source runs exactly what shipped
// before the SOP existed. A task pinned to a version the farm never published is a data
// error and fails closed on the write paths (ports.ErrSOPVersionUnknown).

// SOPPinReader reads the SOP version a task was planned on (pc_care_tasks.sop_version).
type SOPPinReader interface {
	TaskSOPVersion(ctx context.Context, tenantID, taskID string) (int, error)
}

type sopRules struct {
	source ports.SOPRulesSource
	pins   SOPPinReader

	mu    sync.Mutex
	cache map[string]domain.Rules
}

// versionRulesCacheCap bounds the per-process pinned-version cache (tenants x versions).
const versionRulesCacheCap = 256

// WithSOPRules wires the rules source (the out-of-package Postgres adapter) and the pin reader
// (the postgres Repository).
func (s *Service) WithSOPRules(src ports.SOPRulesSource, pins SOPPinReader) *Service {
	s.sop = &sopRules{source: src, pins: pins}
	return s
}

func (s *Service) publishedRules(ctx context.Context, tenantID string) (domain.Rules, error) {
	if s.sop == nil || s.sop.source == nil {
		return domain.SeededRules(), nil
	}
	return s.sop.source.PublishedRules(ctx, tenantID)
}

// rulesForVersion reads one PINNED version's rules. A published version's document is
// immutable, so the read is cached per (tenant, version) for the life of the process; the
// published version itself is never cached here.
func (s *Service) rulesForVersion(ctx context.Context, tenantID string, version int) (domain.Rules, error) {
	if version == 0 || s.sop == nil || s.sop.source == nil {
		return domain.SeededRules(), nil
	}
	key := tenantID + "|" + strconv.Itoa(version)
	s.sop.mu.Lock()
	cached, ok := s.sop.cache[key]
	s.sop.mu.Unlock()
	if ok {
		return cached, nil
	}
	rules, err := s.sop.source.RulesVersion(ctx, tenantID, version)
	if err != nil {
		return domain.Rules{}, err
	}
	s.sop.mu.Lock()
	if s.sop.cache == nil || len(s.sop.cache) >= versionRulesCacheCap {
		// Bounded: a farm cannot publish its way into an unbounded map. Dropping the whole
		// map on overflow is a cold refresh, never a wrong answer.
		s.sop.cache = map[string]domain.Rules{}
	}
	s.sop.cache[key] = rules
	s.sop.mu.Unlock()
	return rules, nil
}

// rulesForTask resolves the rules an EXISTING task runs under from its pin.
func (s *Service) rulesForTask(ctx context.Context, tenantID, taskID string) (domain.Rules, error) {
	if s.sop == nil || s.sop.pins == nil {
		return domain.SeededRules(), nil
	}
	version, err := s.sop.pins.TaskSOPVersion(ctx, tenantID, taskID)
	if err != nil {
		return domain.Rules{}, err
	}
	return s.rulesForVersion(ctx, tenantID, version)
}

// rulesForTaskLenient is the READ-path twin: a task pinned to a version the farm never
// published still has to be workable tonight, so it renders the seeded rules and lets the
// write paths report the pin by name.
func (s *Service) rulesForTaskLenient(ctx context.Context, tenantID, taskID string) (domain.Rules, error) {
	rules, err := s.rulesForTask(ctx, tenantID, taskID)
	if errors.Is(err, ports.ErrSOPVersionUnknown) {
		return domain.SeededRules(), nil
	}
	return rules, err
}

// effectiveRemovalCutoff is the evening a task with these rules runs against: the SOP's own
// when authored, else the farm-wide feed_water_removal_config evening.
func (s *Service) effectiveRemovalCutoff(ctx context.Context, tenantID string, rules domain.Rules) (fwrdomain.Cutoff, error) {
	if own := rules.FeedWaterRemoval.CutoffTime; own != "" {
		cutoff, err := fwrdomain.ParseCutoff(own)
		if err == nil && cutoff.Valid() {
			return cutoff, nil
		}
	}
	return s.removalCutoff(ctx, tenantID)
}

// servedRules is the rule set as CLIENTS read it: every list non-nil, every category filled,
// and the EFFECTIVE removal evening printed so no phone resolves it. A farm with no evening at
// all leaves it blank; the create refuses that by name.
func (s *Service) servedRules(ctx context.Context, tenantID string, rules domain.Rules) domain.Rules {
	out := rules.ServedRules()
	if out.FeedWaterRemoval.Mode == domain.RemovalModeOff {
		return out
	}
	if cutoff, err := s.effectiveRemovalCutoff(ctx, tenantID, rules); err == nil {
		out.FeedWaterRemoval.CutoffTime = cutoff.String()
	}
	return out
}
