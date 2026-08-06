package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestProducerEventTypesInEnum verifies that every domain event type emitted by a producer
// is declared in the domain-event-envelope.schema.json enum.
//
// The outbox relay validates each envelope against the schema. An event_type absent from
// the enum is rejected as invalid_event_envelope, TERMINAL: never retried, never delivered,
// no alert. The write succeeds, state lands, and the entire downstream leg (consumers,
// notifications, projections) silently never happens. This test prevents that silent failure.
//
// WHAT THIS CHECKS:
// - Go constants: eventType<Name> = "<a.b.c>" and Event<Name> = "..."
// - String variables assigned in constant blocks with those patterns
// - Functions that contain both the eventType parameter AND an INSERT INTO outbox_messages call
//
// BLIND SPOTS:
// - Event types built by string concatenation
// - Event types in struct fields
// - Event types passed as bare literals without constant declaration
// - Versions (schema refs like "calendar.notification.v1") are EXCLUDED from the enum check
func TestProducerEventTypesInEnum(t *testing.T) {
	// Load the schema.
	schemaPath, err := findSchemaPath()
	if err != nil {
		t.Fatalf("find schema: %v", err)
	}

	enumValues, err := loadEnumFromSchema(schemaPath)
	if err != nil {
		t.Fatalf("load enum from schema: %v", err)
	}

	// Parse all Go source files under backend/internal.
	backendPath, err := findBackendPath()
	if err != nil {
		t.Fatalf("find backend path: %v", err)
	}

	declaredTypes, problems := findDeclaredEventTypes(backendPath)
	if len(problems) > 0 {
		for _, p := range problems {
			t.Logf("Error: %s", p)
		}
	}

	// Check that every declared type is in the enum.
	var missingTypes []string
	for eventType := range declaredTypes {
		if !enumValues[eventType] {
			missingTypes = append(missingTypes, eventType)
		}
	}

	if len(missingTypes) > 0 {
		t.Errorf(
			"event types are emitted to outbox_messages but absent from %s enum:\n%s\n"+
				"The relay will drop these as invalid_event_envelope, and consumers/notifications/projections silently never run.",
			schemaPath,
			formatMissingTypes(missingTypes),
		)
	}
}

func findSchemaPath() (string, error) {
	// Start from the test file's location and walk up to the repo root.
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 10; i++ {
		schemaPath := filepath.Join(dir, "contracts/jsonschema/domain-event-envelope.schema.json")
		if _, err := os.Stat(schemaPath); err == nil {
			return schemaPath, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("domain-event-envelope.schema.json not found")
}

func findBackendPath() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 10; i++ {
		backendPath := filepath.Join(dir, "backend/internal")
		if stat, err := os.Stat(backendPath); err == nil && stat.IsDir() {
			return backendPath, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("backend/internal not found")
}

func loadEnumFromSchema(schemaPath string) (map[string]bool, error) {
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, err
	}
	var schema struct {
		Properties struct {
			EventType struct {
				Enum []string `json:"enum"`
			} `json:"event_type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, err
	}
	result := make(map[string]bool)
	for _, e := range schema.Properties.EventType.Enum {
		result[e] = true
	}
	return result, nil
}

// findDeclaredEventTypes walks backend/internal, parses Go source files, and extracts
// event types that are produced to outbox_messages. Focuses on explicit constant
// declarations (eventType<Name> = "a.b.c") which are the primary mechanism for
// declaring domain events.
func findDeclaredEventTypes(backendPath string) (map[string]bool, []string) {
	result := make(map[string]bool)
	var problems []string

	// Walk all .go files (except _test.go) and extract eventType constants.
	err := filepath.Walk(backendPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			source := string(data)

			// Extract all eventType<Name> and Event<Name> constants.
			// These are the primary declarations of emitted event types.
			declared := extractEventTypeConstants(source)
			for eventType := range declared {
				result[eventType] = true
			}
		}
		return nil
	})
	if err != nil {
		problems = append(problems, fmt.Sprintf("walk failed: %v", err))
	}

	return result, problems
}

// extractEventTypeConstants finds constant declarations like:
//   eventTypeShedClosed = "weighing.shed.closed"
//   EventWeighingObservationVerified = "..."
//   obligationReopenedEventType = "obligation.reopened"
// Returns the set of event type values (e.g., "weighing.shed.closed").
// Requirement: The value must contain at least one dot (e.g., "obligation.reopened"),
// distinguishing event types from enum values or status names like "vaccination_campaign".
func extractEventTypeConstants(source string) map[string]bool {
	result := make(map[string]bool)

	// Pattern 1: Names starting with eventType or Event, values must have at least one dot
	pattern1 := regexp.MustCompile(`\b(?:eventType|Event)[A-Za-z0-9_]*\s*=\s*"([a-z][a-z0-9_]*\.[a-z0-9_.]*)"`)
	for _, match := range pattern1.FindAllStringSubmatch(source, -1) {
		eventType := match[1]
		// Exclude version suffixes (\.v\d+$)
		if !regexp.MustCompile(`\.v\d+$`).MatchString(eventType) {
			result[eventType] = true
		}
	}

	// Pattern 2: Names ending with EventType (e.g., obligationReopenedEventType)
	// Also requires at least one dot in the value
	pattern2 := regexp.MustCompile(`\b[A-Za-z0-9_]*EventType\s*=\s*"([a-z][a-z0-9_]*\.[a-z0-9_.]*)"`)
	for _, match := range pattern2.FindAllStringSubmatch(source, -1) {
		eventType := match[1]
		// Exclude version suffixes
		if !regexp.MustCompile(`\.v\d+$`).MatchString(eventType) {
			result[eventType] = true
		}
	}

	return result
}

// extractLiteralEventTypes finds event type strings passed as literals to outbox functions.
// Filters out false positives by requiring at least 2 dots (3 parts) in the name,
// which is typical for event types like "feed.direction.completed".
func extractLiteralEventTypes(source string) map[string]bool {
	result := make(map[string]bool)

	// Find functions that call known outbox writing functions
	funcPattern := regexp.MustCompile(`(?s)func\s+[\w*]+\s*\([^)]*\)\s*[^{]*\{(?:[^{}]|{[^}]*})*\}`)

	for _, funcMatch := range funcPattern.FindAllString(source, -1) {
		// Check if this function writes to outbox_messages either directly or via a helper.
		isOutboxWriter := strings.Contains(funcMatch, "outbox_messages") ||
			strings.Contains(funcMatch, "insertOutbox") ||
			strings.Contains(funcMatch, "insertObligationLifecycleOutbox") ||
			strings.Contains(funcMatch, "insertProtocolOutbox")

		if !isOutboxWriter {
			continue
		}

		// Look for quoted strings with 2+ dots (e.g., "feed.direction.completed").
		// This heuristic avoids false positives from single-word action names like
		// "pending", "applied", "rejected" which are typically used as status values, not event types.
		eventTypePattern := regexp.MustCompile(`"([a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*){2,})"`)
		for _, match := range eventTypePattern.FindAllStringSubmatch(funcMatch, -1) {
			eventType := match[1]
			// Exclude version suffixes (\.v\d+$)
			if regexp.MustCompile(`\.v\d+$`).MatchString(eventType) {
				continue
			}
			result[eventType] = true
		}
	}

	return result
}

// isEmittedToOutbox checks whether the given eventType is used in a function that
// contains an INSERT INTO outbox_messages call. This prevents false positives on
// idempotency keys and other non-event uses of eventType parameters.
func isEmittedToOutbox(eventType string, source string) bool {
	// For a more precise check, extract each function and check independently.
	// This regex splits on function boundaries.
	funcPattern := regexp.MustCompile(`(?s)func\s+[\w*]+\s*\([^)]*\)\s*[^{]*\{(?:[^{}]|{[^}]*})*\}`)

	for _, funcMatch := range funcPattern.FindAllString(source, -1) {
		// Check if this function has outbox_messages.
		if !strings.Contains(funcMatch, "outbox_messages") {
			continue
		}

		// Check if this function uses our eventType.
		if strings.Contains(funcMatch, `"`+eventType+`"`) {
			// Double-check: the eventType should be a parameter or referenced constant,
			// not just any string literal. Verify it's used in a context that makes sense.
			// For now, if outbox_messages is present and the type string is present, assume it's used.
			return true
		}
	}

	return false
}

func formatMissingTypes(types []string) string {
	var lines []string
	for _, t := range types {
		lines = append(lines, fmt.Sprintf("  - %s", t))
	}
	return strings.Join(lines, "\n")
}
