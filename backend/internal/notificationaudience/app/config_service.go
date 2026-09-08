package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/notificationaudience/domain"
	"github.com/vgoats/goatos/backend/internal/notificationaudience/ports"
)

// ErrInvalidRequest signals a malformed save. Wrapped with a farm-readable reason.
var ErrInvalidRequest = errors.New("invalid notification audience request")

// ConfigService serves the admin matrix: every configurable alert, every designation, and the
// effective audience of each alert. Every visible word is composed here from the catalog and
// the designation table; the client renders them verbatim.
type ConfigService struct {
	repo ports.AudienceRepository
}

func NewConfigService(repo ports.AudienceRepository) *ConfigService {
	return &ConfigService{repo: repo}
}

// DesignationOption is one column of the matrix.
type DesignationOption struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Grade string `json:"grade,omitempty"`
}

// ModuleOption is one row group of the matrix.
type ModuleOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// AlertRow is one row of the matrix.
type AlertRow struct {
	Key         string `json:"key"`
	Module      string `json:"module"`
	ModuleLabel string `json:"module_label"`
	Label       string `json:"label"`
	Blurb       string `json:"blurb"`
	// DefaultDesignations is the catalog default; Designations is what applies today. They
	// are equal unless Customised.
	DefaultDesignations []string `json:"default_designations"`
	Designations        []string `json:"designations"`
	Customised          bool     `json:"customised"`
	// RowVersion fences a save: 0 for a not-yet-customised alert.
	RowVersion int `json:"row_version"`
}

// MatrixResponse is the whole screen in one read.
type MatrixResponse struct {
	Designations []DesignationOption `json:"designations"`
	Modules      []ModuleOption      `json:"modules"`
	Alerts       []AlertRow          `json:"alerts"`
}

// SaveAudienceRequest replaces one alert's audience. UseDefaults discards the override.
type SaveAudienceRequest struct {
	DesignationCodes []string `json:"designation_codes"`
	UseDefaults      bool     `json:"use_defaults"`
	RowVersion       int      `json:"row_version"`
}

// Matrix builds the whole screen: designations (columns), modules (groups), alerts (rows).
func (s *ConfigService) Matrix(ctx context.Context, tenantID string) (MatrixResponse, error) {
	designations, err := s.repo.ListDesignations(ctx)
	if err != nil {
		return MatrixResponse{}, err
	}
	overrides, err := s.repo.ListAudiences(ctx, tenantID)
	if err != nil {
		return MatrixResponse{}, err
	}
	out := MatrixResponse{
		Designations: make([]DesignationOption, 0, len(designations)),
		Modules:      make([]ModuleOption, 0, len(domain.Modules)),
		Alerts:       make([]AlertRow, 0, 32),
	}
	for _, d := range designations {
		out.Designations = append(out.Designations, DesignationOption{Code: d.Code, Label: d.Label, Grade: d.Grade})
	}
	for _, m := range domain.Modules {
		out.Modules = append(out.Modules, ModuleOption{Key: m.Key, Label: m.Label})
	}
	for _, alert := range domain.Catalog() {
		out.Alerts = append(out.Alerts, alertRow(alert, overrides[alert.Key]))
	}
	return out, nil
}

func alertRow(alert domain.Alert, override ports.Audience) AlertRow {
	row := AlertRow{
		Key:                 alert.Key,
		Module:              alert.Module,
		ModuleLabel:         domain.ModuleLabel(alert.Module),
		Label:               alert.Label,
		Blurb:               alert.Blurb,
		DefaultDesignations: append([]string{}, alert.DefaultDesignations...),
		Designations:        append([]string{}, alert.DefaultDesignations...),
	}
	if override.AlertKey != "" {
		row.Customised = true
		row.Designations = append([]string{}, override.DesignationCodes...)
		row.RowVersion = override.RowVersion
	}
	return row
}

// Save replaces one alert's audience, or resets it to the catalog default, and returns the
// row as it now reads.
func (s *ConfigService) Save(ctx context.Context, tenantID, actorID, alertKey string, req SaveAudienceRequest) (AlertRow, error) {
	alert, ok := domain.AlertByKey(alertKey)
	if !ok {
		return AlertRow{}, fmt.Errorf("%w: %q", ports.ErrUnknownAlert, alertKey)
	}
	if req.RowVersion < 0 {
		return AlertRow{}, fmt.Errorf("%w: reload the page to see the current settings, then try again", ErrInvalidRequest)
	}
	if req.UseDefaults {
		if err := s.repo.ResetAudience(ctx, tenantID, actorID, alert.Key, req.RowVersion); err != nil {
			return AlertRow{}, err
		}
		return alertRow(alert, ports.Audience{}), nil
	}
	codes := normaliseCodes(req.DesignationCodes)
	stored, err := s.repo.ReplaceAudience(ctx, ports.ReplaceAudienceCommand{
		TenantID:           tenantID,
		ActorID:            actorID,
		AlertKey:           alert.Key,
		DesignationCodes:   codes,
		ExpectedRowVersion: req.RowVersion,
	})
	if err != nil {
		return AlertRow{}, err
	}
	return alertRow(alert, stored), nil
}

// normaliseCodes trims, drops blanks and duplicates, and sorts, so two admins ticking the same
// desks in a different order store the same row.
func normaliseCodes(codes []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		code = strings.ToLower(strings.TrimSpace(code))
		if code == "" {
			continue
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}
