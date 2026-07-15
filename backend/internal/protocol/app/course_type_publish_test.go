package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// TestPublishRejectsBlankCourseTypeInMatrixRows proves publish validation
// rejects a matrix where individual vaccine rows have blank course_type.
// Individual vaccines (matrix_rows[].vaccine) MUST carry explicit course_type;
// only the matrix wrapper (type=matrix) is exempt. A valid matrix with
// course_type=single or booster for all rows must still publish.
func TestPublishRejectsBlankCourseTypeInMatrixRows(t *testing.T) {
	base := validVaccinationMatrixRulesetDSL()
	if !strings.Contains(base, `"course_type":"single"`) {
		t.Fatalf("test fixture drift: expected a course_type in the canonical matrix ruleset")
	}

	version := func(ruleDSL string) domain.Version {
		return domain.Version{
			SopVersionID: "62000000-0000-4000-8000-000000000001",
			ProofPolicy:  []byte(`{"required":true,"types":["video"]}`),
			RuleDsl:      []byte(ruleDSL),
		}
	}

	// Sanity: the reviewed matrix is publishable.
	if err := ValidateExecutionContract(version(base)); err != nil {
		t.Fatalf("reviewed matrix ruleset must be publishable, got: %v", err)
	}

	t.Run("matrix_row_missing_course_type", func(t *testing.T) {
		// Remove course_type from the matrix row vaccine
		malformed := strings.Replace(base, `,"course_type":"single"`, ``, 1)
		err := ValidateExecutionContract(version(malformed))
		if !errors.Is(err, ErrNotPublishable) {
			t.Fatalf("matrix_rows[].vaccine missing course_type must fail publish with ErrNotPublishable, got: %v", err)
		}
		if !strings.Contains(err.Error(), "course type required") {
			t.Fatalf("error must mention course type requirement, got: %v", err)
		}
	})

	t.Run("matrix_row_blank_course_type", func(t *testing.T) {
		// Set course_type to empty string
		malformed := strings.Replace(base, `"course_type":"single"`, `"course_type":""`, 1)
		err := ValidateExecutionContract(version(malformed))
		if !errors.Is(err, ErrNotPublishable) {
			t.Fatalf("matrix_rows[].vaccine with blank course_type must fail publish with ErrNotPublishable, got: %v", err)
		}
		if !strings.Contains(err.Error(), "course type required") {
			t.Fatalf("error must mention course type requirement, got: %v", err)
		}
	})

	t.Run("matrix_row_invalid_course_type", func(t *testing.T) {
		malformed := strings.Replace(base, `"course_type":"single"`, `"course_type":"invalid"`, 1)
		err := ValidateExecutionContract(version(malformed))
		if !errors.Is(err, ErrNotPublishable) {
			t.Fatalf("matrix_rows[].vaccine with invalid course_type must fail publish with ErrNotPublishable, got: %v", err)
		}
		if !strings.Contains(err.Error(), "course_type") && !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("error must mention course type or invalid value, got: %v", err)
		}
	})

	t.Run("matrix_row_valid_booster", func(t *testing.T) {
		malformed := strings.Replace(base, `"course_type":"single"`, `"course_type":"booster"`, 1)
		err := ValidateExecutionContract(version(malformed))
		if err != nil {
			t.Fatalf("matrix_rows[].vaccine with course_type=booster must publish, got: %v", err)
		}
	})
}

// TestPublishRejectsBlankPathogenClassInMatrixRows proves publish validation
// rejects a matrix where individual vaccine rows have blank pathogen_class.
// Individual vaccines (matrix_rows[].vaccine) MUST carry explicit pathogen_class;
// only the matrix wrapper (type=matrix) is exempt. A valid matrix with
// pathogen_class=viral or bacterial for all rows must still publish.
func TestPublishRejectsBlankPathogenClassInMatrixRows(t *testing.T) {
	base := validVaccinationMatrixRulesetDSL()
	if !strings.Contains(base, `"pathogen_class":"viral"`) {
		t.Fatalf("test fixture drift: expected a pathogen_class in the canonical matrix ruleset")
	}

	version := func(ruleDSL string) domain.Version {
		return domain.Version{
			SopVersionID: "62000000-0000-4000-8000-000000000001",
			ProofPolicy:  []byte(`{"required":true,"types":["video"]}`),
			RuleDsl:      []byte(ruleDSL),
		}
	}

	// Sanity: the reviewed matrix is publishable.
	if err := ValidateExecutionContract(version(base)); err != nil {
		t.Fatalf("reviewed matrix ruleset must be publishable, got: %v", err)
	}

	t.Run("matrix_row_missing_pathogen_class", func(t *testing.T) {
		// Remove pathogen_class from the matrix row vaccine
		malformed := strings.Replace(base, `,"pathogen_class":"viral"`, ``, 1)
		err := ValidateExecutionContract(version(malformed))
		if !errors.Is(err, ErrNotPublishable) {
			t.Fatalf("matrix_rows[].vaccine missing pathogen_class must fail publish with ErrNotPublishable, got: %v", err)
		}
		if !strings.Contains(err.Error(), "pathogen class required") {
			t.Fatalf("error must mention pathogen class requirement, got: %v", err)
		}
	})

	t.Run("matrix_row_blank_pathogen_class", func(t *testing.T) {
		// Set pathogen_class to empty string
		malformed := strings.Replace(base, `"pathogen_class":"viral"`, `"pathogen_class":""`, 1)
		err := ValidateExecutionContract(version(malformed))
		if !errors.Is(err, ErrNotPublishable) {
			t.Fatalf("matrix_rows[].vaccine with blank pathogen_class must fail publish with ErrNotPublishable, got: %v", err)
		}
		if !strings.Contains(err.Error(), "pathogen class required") {
			t.Fatalf("error must mention pathogen class requirement, got: %v", err)
		}
	})

	t.Run("matrix_row_invalid_pathogen_class", func(t *testing.T) {
		malformed := strings.Replace(base, `"pathogen_class":"viral"`, `"pathogen_class":"unknown"`, 1)
		err := ValidateExecutionContract(version(malformed))
		if !errors.Is(err, ErrNotPublishable) {
			t.Fatalf("matrix_rows[].vaccine with invalid pathogen_class must fail publish with ErrNotPublishable, got: %v", err)
		}
		if !strings.Contains(err.Error(), "pathogen class") && !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("error must mention pathogen class or invalid value, got: %v", err)
		}
	})

	t.Run("matrix_row_valid_bacterial", func(t *testing.T) {
		malformed := strings.Replace(base, `"pathogen_class":"viral"`, `"pathogen_class":"bacterial"`, 1)
		err := ValidateExecutionContract(version(malformed))
		if err != nil {
			t.Fatalf("matrix_rows[].vaccine with pathogen_class=bacterial must publish, got: %v", err)
		}
	})
}

// TestPublishAllowsMatrixWrapperWithoutCourseType proves publish validation
// allows the matrix wrapper (code=vaccination.matrix, type=matrix) to omit
// course_type, since the wrapper itself is not an individual vaccine.
func TestPublishAllowsMatrixWrapperWithoutCourseType(t *testing.T) {
	base := validVaccinationMatrixRulesetDSL()

	version := func(ruleDSL string) domain.Version {
		return domain.Version{
			SopVersionID: "62000000-0000-4000-8000-000000000001",
			ProofPolicy:  []byte(`{"required":true,"types":["video"]}`),
			RuleDsl:      []byte(ruleDSL),
		}
	}

	// Remove course_type from the wrapper (the outer vaccine object at the top level)
	// The wrapper is identified by code="vaccination.matrix" and type="matrix"
	t.Run("wrapper_missing_course_type_ok", func(t *testing.T) {
		// This test verifies the wrapper (not matrix_rows[].vaccine) can omit course_type
		// We need to construct a valid matrix but remove the wrapper's course_type if it had one.
		// The current test fixture doesn't include course_type at the wrapper level, so this is already passing.
		err := ValidateExecutionContract(version(base))
		if err != nil {
			t.Fatalf("wrapper without course_type must be allowed, got: %v", err)
		}
	})
}
