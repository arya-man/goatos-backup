package sg.mesha.goatos.core.data.capture

import sg.mesha.goatos.core.data.executionPartitionKey

/**
 * The outbox ordering lane for ONE vaccination shed session.
 *
 * Every write that belongs to the same session -- each animal's scan capture, and the
 * session's final Submit -- must be enqueued under this exact key, because the sync
 * engine only holds a row back behind an earlier failure IN THE SAME GROUP. Two
 * dependent writes in two lanes are not ordered at all.
 *
 * That is precisely what went wrong here. Scans used `"$taskId|$partitionKey"` while
 * Submit used `"${shedId}|${label.trim().lowercase()}"` -- a different first segment AND
 * a different normalisation ("Part 2" became "2" on one side and "part 2" on the other).
 * So a scan could sit FAILED in one lane while the Submit drained in another and closed
 * the shed one animal short, with the shed reading as complete so nobody went looking.
 *
 * This lives in one tested function rather than being re-typed at each call site, which
 * is the same reason feed's key does (FeedCaptureGroupKey.kt): re-typing is how the two
 * sides drifted apart in the first place. Weighing groups its per-animal observations and
 * its scope submit under one identity for the same reason (WeighingRepository.kt).
 *
 * The value is deliberately identical to what scan capture already produced, so no queued
 * row changes lane on upgrade and no Room migration is needed -- the persisted scan rows
 * carry no shed id, and the Submit side is the one that moves onto this key.
 */
fun vaccinationSessionGroupKey(taskId: String, partitionLabel: String?): String =
    "$taskId|${executionPartitionKey(partitionLabel)}"
