package postgres

import "strings"

// TWO partition keys exist in this module, and binding the wrong one against a column matches
// NOTHING while every test with a one-token label ("1", "3") stays green.
//
//   - domain.PartitionMatchKey is the CONFIG-KEY form: separators collapsed to '_'
//     ("Part 3" -> "part_3"). It is what feed_experiment_config.partition_key stores
//     (feed_config_norm) and what every in-memory pen key (domain.PenKey, the row grouping maps)
//     is built from.
//   - The GENERATED partition_key column of feed_direction_issue_rows,
//     feed_distribution_completions, feed_packing_completions and feed_wastage_completions is
//     lower(btrim(partition_label)), 'whole' when blank ("Part 3" -> "part 3").
//
// Found on the QA clone 2026-09-16: a second operator's submit for "Godel 2 - Part 1" answered
// 500 ("read existing distribution completion: no rows") because the natural-key conflict
// re-read bound "part_1" against a column holding "part 1"; the afternoon reopen
// (ReopenPackingForFeedChange) bound the same form and therefore never reopened a Part-N pen.
// Castro "1" / "2" pens never hit it, which is why the field saw it only on partitioned sheds.
//
// [partitionColumnKey] is the ONLY way this package derives a bind for those columns from a raw
// label; [partitionKeyAsConfigKeySQL] is the ONLY way it compares one of those columns against
// an in-memory PenKey. Do not bind domain.PartitionMatchKey against `partition_key` again.
func partitionColumnKey(label string) string {
	// btrim default: ASCII space only -- mirror it exactly rather than TrimSpace.
	trimmed := strings.Trim(label, " ")
	if trimmed == "" {
		return "whole"
	}
	return strings.ToLower(trimmed)
}

// partitionKeyAsConfigKeySQL renders a column expression that turns a generated partition_key
// into the config-key form domain.PartitionMatchKey produces, so an in-memory PenKey can be
// compared against it. feed_config_norm is the SQL twin of domain.NormalizeConfigKey (migration
// 000001) and the 'whole' sentinel is passed through unchanged.
func partitionKeyAsConfigKeySQL(column string) string {
	return "CASE WHEN " + column + " = 'whole' THEN 'whole' ELSE feed_config_norm(" + column + ") END"
}
