package outbox

import (
	"crypto/md5"
	"fmt"
)

// DeterministicUUID returns an RFC 4122 version-3-style UUID for outbox events
// that must keep the same event_id across retries.
func DeterministicUUID(seed string) string {
	sum := md5.Sum([]byte(seed))
	sum[6] = (sum[6] & 0x0f) | 0x30
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
