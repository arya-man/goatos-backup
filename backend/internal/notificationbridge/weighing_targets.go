package notificationbridge

import "strings"

// Deep-link targets for weighing pushes.
//
// Every weighing push used to carry the bare module landing "/weighing". That route is the
// OPERATOR's own work screen, so a Growth Director or CEO who tapped a push about a task they
// oversee was dropped into "My Work" -- a screen listing the sheds assigned to THEM, which for a
// director is empty -- with no way back to the task or the evidence the push was about. The push
// already knows exactly which task and which bucket it is about, so it can name it.
//
// The three shapes below are the ones the Android build actually hosts as push destinations
// (Routes.WEIGHING_TASK / WEIGHING_SHED / WEIGHING_ALERTS in pushTargetDestinations, parsed by
// pushTargetRoute); emitting anything else would fall through to the recipient's own landing and
// reproduce the defect. Query-parameter names are the app's nav argument names -- `campaignId`
// and `campaignShedId` -- and are the contract this file shares with it.
const (
	weighingModuleTarget = "/weighing"
	// weighingEvidenceTarget is where a proof-shaped push without campaign/bucket identity lands.
	// It used to be the leadership proof gallery (/weighing/videos), which was retired from
	// mobile on 2026-08-28; the module's own alerts feed is the surviving surface that lists
	// what these pushes announce.
	weighingEvidenceTarget = "/weighing/alerts"
)

// weighingTaskTarget deep-links ONE weighing task (one park on one weigh date), which is the unit
// of work a task-level push (published, task closed) is actually about.
func weighingTaskTarget(campaignID string) string {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return weighingModuleTarget
	}
	return "/weighing/task?campaignId=" + campaignID
}

// weighingBucketTarget deep-links ONE shed bucket's read-only record, which is where the evidence
// a bucket-level push reports on (a verdict, a submission, a close) actually lives.
//
// Falls back to the task, and then to the module, rather than emitting a half-built link: a
// destination missing its bucket id renders an empty screen, which is worse than the landing.
func weighingBucketTarget(campaignID, campaignShedID string) string {
	campaignShedID = strings.TrimSpace(campaignShedID)
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" || campaignShedID == "" {
		return weighingTaskTarget(campaignID)
	}
	return "/weighing/shed?campaignId=" + campaignID + "&campaignShedId=" + campaignShedID
}
