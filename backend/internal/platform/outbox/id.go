package outbox

import (
	"crypto/md5" // #nosec G501 -- deterministic identifier only, not a security hash.
	"fmt"
)

// DeterministicUUID returns a v3-shaped, non-namespaced UUID for outbox events
// that must keep byte-identical event_id values across retries.
func DeterministicUUID(seed string) string {
	// #nosec G401 -- deterministic identifier only, not a security hash.
	sum := md5.Sum([]byte(seed))
	sum[6] = (sum[6] & 0x0f) | 0x30
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
