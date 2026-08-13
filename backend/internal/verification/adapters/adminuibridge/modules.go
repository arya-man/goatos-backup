// Package adminuibridge exposes the Verification type registry to the admin-web contract builder
// as the source of the verifier-only workspace's evidence modules.
//
// It is the same seam the mobile drawer already uses, pointed at admin-web: a producer that
// registers a category with navigation metadata appears in the verifier's web sidebar with no
// change to adminui. Direction of dependency is deliberate -- verification knows about the adminui
// port type, adminui knows nothing about verification -- so the admin-web contract package stays
// free of module-specific imports.
package adminuibridge

import (
	adminuiapp "github.com/vgoats/goatos/backend/internal/adminui/app"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// CategorySource is the slice of the Verification service this bridge needs.
type CategorySource interface {
	Categories() []domain.CategoryDefinition
}

// Source adapts the registry to adminuiapp.VerificationModuleSource.
type Source struct {
	categories CategorySource
}

func New(categories CategorySource) *Source {
	return &Source{categories: categories}
}

// VerifierNavModules groups registered categories into drawer modules by NavigationModule.
//
// A category with no navigation metadata is skipped rather than defaulted into some module: the
// registry validates those four fields as all-or-nothing (Registry.Register), so a blank
// NavigationModule means the producer has not declared where its evidence belongs, and guessing
// would put a queue under a module the maintainer never approved.
func (s *Source) VerifierNavModules() []adminuiapp.VerificationNavModule {
	if s == nil || s.categories == nil {
		return nil
	}
	byModule := map[string]*adminuiapp.VerificationNavModule{}
	order := make([]string, 0, 8)
	for _, def := range s.categories.Categories() {
		if def.NavigationModule == "" || def.NavigationModuleLabel == "" || def.PageKey == "" || def.PageLabel == "" {
			continue
		}
		module, ok := byModule[def.NavigationModule]
		if !ok {
			module = &adminuiapp.VerificationNavModule{
				Key:   def.NavigationModule,
				Label: def.NavigationModuleLabel,
			}
			byModule[def.NavigationModule] = module
			order = append(order, def.NavigationModule)
		}
		module.Pages = append(module.Pages, adminuiapp.VerificationNavPage{
			Key:      def.PageKey,
			Label:    def.PageLabel,
			Category: def.Category,
			Order:    def.PageOrder,
		})
	}
	out := make([]adminuiapp.VerificationNavModule, 0, len(order))
	for _, key := range order {
		out = append(out, *byModule[key])
	}
	return out
}
