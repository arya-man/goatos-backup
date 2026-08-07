// Package domain defines the Health module's disease-course and treatment-work contracts.
package domain

import "time"

const (
	AgeBandAdult        = "adult"
	AgeBandKid          = "kid"
	SessionMorning      = "morning"
	SessionAfternoon    = "afternoon"
	SessionEvening      = "evening"
	SessionUnscheduled  = "unscheduled"
	DefaultDurationDays = 3
	MaxDurationDays     = 90
	MaxPageSize         = 20
)

type ProtocolStep struct {
	StepID             string  `json:"step_id,omitempty"`
	DayNo              int     `json:"day_no"`
	Session            string  `json:"session"`
	Seq                int     `json:"seq"`
	RecordType         string  `json:"record_type"`
	MedicineName       *string `json:"medicine_name"`
	DosageText         *string `json:"dosage_text"`
	DosageDenominator  *string `json:"dosage_denominator"`
	MedicineRoute      *string `json:"medicine_route"`
	Instruction        *string `json:"instruction"`
	CriticalActionType *string `json:"critical_action_type"`
	Status             string  `json:"status,omitempty"`
}

type Protocol struct {
	ProtocolVersionID string         `json:"protocol_version_id"`
	DiseaseKey        string         `json:"disease_key"`
	DisplayName       string         `json:"display_name"`
	AgeBand           string         `json:"age_band"`
	Version           int            `json:"version"`
	DurationDays      int            `json:"duration_days"`
	Steps             []ProtocolStep `json:"steps"`
}

type OpenCaseInput struct {
	TenantID           string
	ActorID            string
	GoatID             string
	DiseaseKey         string
	AgeBand            string
	StartDate          time.Time
	IdempotencyKey     string
	RequestFingerprint string
	TraceID            string
}
type OpenCaseResult struct {
	CaseID           string `json:"case_id"`
	FirstSessionID   string `json:"first_session_id"`
	SessionCount     int    `json:"session_count"`
	DurationDays     int    `json:"duration_days"`
	IdempotentReplay bool   `json:"idempotent_replay"`
}

type WorkItem struct {
	SessionID                  string    `json:"health_session_id"`
	CaseID                     string    `json:"case_id"`
	GoatID                     string    `json:"goat_id"`
	GoatDisplayID              string    `json:"goat_display_id"`
	DiseaseKey                 string    `json:"disease_key"`
	DiseaseName                string    `json:"disease_name"`
	AgeBand                    string    `json:"age_band"`
	DayNo                      int       `json:"day_no"`
	DurationDays               int       `json:"duration_days"`
	BusinessDate               string    `json:"business_date"`
	Session                    string    `json:"session"`
	DueAt                      time.Time `json:"due_at"`
	Status                     string    `json:"status"`
	ParkID                     *string   `json:"park_id"`
	ParkLabel                  string    `json:"park_label"`
	ShedID                     *string   `json:"shed_id"`
	ShedLabel                  string    `json:"shed_label"`
	PartitionLabel             string    `json:"partition_label"`
	OperationalLocationDisplay string    `json:"operational_location_display"`
	StepCount                  int       `json:"step_count"`
	MedicationCount            int       `json:"medication_count"`
	HasCriticalStep            bool      `json:"has_critical_step"`
}
type Summary struct {
	Total         int `json:"total"`
	Due           int `json:"due"`
	Scheduled     int `json:"scheduled"`
	InProgress    int `json:"in_progress"`
	Completed     int `json:"completed"`
	Rework        int `json:"rework"`
	Held          int `json:"held"`
	CanceledDeath int `json:"canceled_death"`
}
type DateMarker struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}
type FilterOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}
type FilterOptions struct {
	Diseases []FilterOption `json:"diseases"`
	Parks    []FilterOption `json:"parks"`
	Sheds    []FilterOption `json:"sheds"`
}
type ListFilter struct {
	TenantID   string
	AgeBand    string
	Date       string
	Status     string
	DiseaseKey string
	ParkID     string
	ShedID     string
	Session    string
	Cursor     string
	Limit      int
	MarkerFrom string
	MarkerTo   string
}
type WorkItemPage struct {
	Items         []WorkItem    `json:"items"`
	Summary       Summary       `json:"summary"`
	DateMarkers   []DateMarker  `json:"date_markers"`
	FilterOptions FilterOptions `json:"filter_options"`
	NextCursor    *string       `json:"next_cursor"`
}
type WorkItemDetail struct {
	WorkItem
	Steps []ProtocolStep `json:"steps"`
}
type CompleteInput struct {
	TenantID           string
	ActorID            string
	SessionID          string
	ProofRef           string
	IdempotencyKey     string
	RequestFingerprint string
	TraceID            string
}
type CompleteResult struct {
	SessionID        string    `json:"health_session_id"`
	Status           string    `json:"status"`
	CompletedAt      time.Time `json:"completed_at"`
	MedicationCount  int       `json:"medication_count"`
	IdempotentReplay bool      `json:"idempotent_replay"`
}
type SourceProtocol struct {
	DiseaseKey             string         `json:"disease_key"`
	DisplayName            string         `json:"display_name"`
	AgeBand                string         `json:"age_band"`
	DurationDays           int            `json:"duration_days"`
	DefaultDurationApplied bool           `json:"default_duration_applied"`
	Steps                  []ProtocolStep `json:"steps"`
}
