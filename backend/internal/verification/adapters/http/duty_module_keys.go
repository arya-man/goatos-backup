package http

import "github.com/vgoats/goatos/backend/internal/verification"

// navigationModuleForDutyCode wraps the shared verification translation function.
func navigationModuleForDutyCode(dutyModuleCode string) string {
	return verification.NavigationModuleForDutyCode(dutyModuleCode)
}
