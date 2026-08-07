// Package verificationbridge holds the composition-layer adapter that connects the counts
// module to the verification module's service API without either module importing the other's storage.
package verificationbridge

import (
	"context"

	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// verificationCreator is the slice of the verification service this bridge needs: enqueue one item.
type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

func ptrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
