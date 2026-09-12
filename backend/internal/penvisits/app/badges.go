package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
)

// The pen visit is the care work's last step (maintainer decision 2026-09-12), so its badge
// rides the PARENT modules: a visit still to record counts on Preventive Care (on the tab of
// its first care reason) or on Vaccination when the pen was only vaccinated. The Tasks
// module's badge is the leadership-tasks source's own again -- the "For me" tab is gone.
const (
	ModuleKeyPCCare      = "pc_care"
	ModuleKeyVaccination = "vaccination"
)

// tabHrefForReason is the PC Care bar tab a care reason lives under (bootstrap_copy.go's
// contributions); vaccination has no tab of its own here.
var tabHrefForReason = map[string]string{
	domain.ReasonDeworming:     "/pc/deworming",
	domain.ReasonAntiProtozoan: "/pc/anti-protozoan",
	domain.ReasonTicksRemoval:  "/pc/ticks",
	domain.ReasonHoofTrimming:  "/pc/hoof-trimming",
	domain.ReasonHairTrimming:  "/pc/hair-trimming",
}

// BadgeSource is the shape workforce/app.ModuleBadgeSource asks for; the leadership tasks
// service implements it for its own module key.
type BadgeSource interface {
	ModuleBadgeCounts(ctx context.Context, tenantID, userID string, moduleKeys []string) (map[string]int, error)
}

// NavItemBadgeSource is the per-tab half (workforce/app.NavItemBadgeSource).
type NavItemBadgeSource interface {
	NavItemBadgeCounts(ctx context.Context, tenantID, userID string, hrefs []string) (map[string]int, error)
}

// ModuleBadges decorates the leadership-tasks badge source with the pen visits still to
// record, filed under the parent modules.
type ModuleBadges struct {
	inner  BadgeSource
	visits *Service
}

// NewModuleBadges composes the badge source.
func NewModuleBadges(inner BadgeSource, visits *Service) *ModuleBadges {
	return &ModuleBadges{inner: inner, visits: visits}
}

// split files each open visit under ONE module and ONE tab: the first care reason in
// display order wins; a pen vaccinated alone files under vaccination with no tab.
func split(reasonSets [][]string) (modules map[string]int, tabs map[string]int) {
	modules = map[string]int{}
	tabs = map[string]int{}
	for _, reasons := range reasonSets {
		filed := false
		for _, r := range domain.SortReasons(reasons) {
			if href, ok := tabHrefForReason[r]; ok {
				modules[ModuleKeyPCCare]++
				tabs[href]++
				filed = true
				break
			}
		}
		if !filed {
			modules[ModuleKeyVaccination]++
		}
	}
	return modules, tabs
}

// ModuleBadgeCounts implements workforce/app.ModuleBadgeSource.
func (b *ModuleBadges) ModuleBadgeCounts(ctx context.Context, tenantID, userID string, moduleKeys []string) (map[string]int, error) {
	out := map[string]int{}
	if b.inner != nil {
		counts, err := b.inner.ModuleBadgeCounts(ctx, tenantID, userID, moduleKeys)
		if err != nil {
			return nil, err
		}
		for k, v := range counts {
			out[k] = v
		}
	}
	wanted := false
	for _, k := range moduleKeys {
		if k == ModuleKeyPCCare || k == ModuleKeyVaccination {
			wanted = true
			break
		}
	}
	if !wanted || b.visits == nil {
		return out, nil
	}
	reasonSets, err := b.visits.OpenReasons(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	modules, _ := split(reasonSets)
	for _, k := range moduleKeys {
		if n := modules[k]; n > 0 {
			out[k] += n
		}
	}
	return out, nil
}

// NavItemBadgeCounts implements workforce/app.NavItemBadgeSource: each PC Care category tab
// carries the visits owed for that work; other tabs are delegated to the inner source.
func (b *ModuleBadges) NavItemBadgeCounts(ctx context.Context, tenantID, userID string, hrefs []string) (map[string]int, error) {
	out := map[string]int{}
	if inner, ok := b.inner.(NavItemBadgeSource); ok && inner != nil {
		counts, err := inner.NavItemBadgeCounts(ctx, tenantID, userID, hrefs)
		if err != nil {
			return nil, err
		}
		for k, v := range counts {
			out[k] = v
		}
	}
	wanted := false
	for _, h := range hrefs {
		for _, tab := range tabHrefForReason {
			if h == tab {
				wanted = true
			}
		}
	}
	if !wanted || b.visits == nil {
		return out, nil
	}
	reasonSets, err := b.visits.OpenReasons(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	_, tabs := split(reasonSets)
	for _, h := range hrefs {
		if n := tabs[h]; n > 0 {
			out[h] += n
		}
	}
	return out, nil
}
