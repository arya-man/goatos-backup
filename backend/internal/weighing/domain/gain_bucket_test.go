package domain

import "testing"

// The read cache memoises an analytics answer per (tenant, parks, window, sex, origin, mode). The
// Time-wise scope MUST be part of that key: without it a request for ONE PEN is served the whole
// farm's cached answer, and a page that had just been read unfiltered shows every pen under a
// heading naming one. That is not hypothetical -- it is what the first browser run of this feature
// showed, with the page narrowing nothing while the API narrowed correctly when called directly.
func TestTimeScopeCacheKeySeparatesEveryScopeThatChangesTheAnswer(t *testing.T) {
	seen := map[string]string{}
	for name, scope := range map[string]TimeScope{
		"week, every pen":  {Bucket: GainBucketWeek},
		"month, every pen": {Bucket: GainBucketMonth},
		"week, pen A":      {Bucket: GainBucketWeek, PenLocationID: "0c3f5b8a-1111-4a4c-9c2f-4c2d9c3f7a11", PenPartitionLabel: "Part A"},
		"week, pen B":      {Bucket: GainBucketWeek, PenLocationID: "0c3f5b8a-1111-4a4c-9c2f-4c2d9c3f7a11", PenPartitionLabel: "Part B"},
		"week, other shed": {Bucket: GainBucketWeek, PenLocationID: "0c3f5b8a-2222-4a4c-9c2f-4c2d9c3f7a11", PenPartitionLabel: "Part A"},
		"month, pen A":     {Bucket: GainBucketMonth, PenLocationID: "0c3f5b8a-1111-4a4c-9c2f-4c2d9c3f7a11", PenPartitionLabel: "Part A"},
	} {
		key := scope.CacheKey()
		if other, clash := seen[key]; clash {
			t.Fatalf("%q and %q share the cache key %q, so one would be served the other's answer", name, other, key)
		}
		seen[key] = name
	}
	// An absent bucket is the week default, so the unfiltered page keeps ONE key rather than two
	// that hold the same answer.
	if (TimeScope{}).CacheKey() != (TimeScope{Bucket: GainBucketWeek}).CacheKey() {
		t.Fatal("a blank bucket and an explicit week must share one cache entry")
	}
}
