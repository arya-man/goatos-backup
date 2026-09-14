package domain

import "github.com/vgoats/goatos/backend/internal/tasks/domain/sopseed"

// TaskTypeRegistry is the compiler's view of the Task Type Registry, keyed by task type key.
type TaskTypeRegistry map[string]FollowUpTaskTy

// SeededTaskTypes returns the registry as seeded by migration 000308. The postgres adapter reads
// the live `sop_task_types` rows instead; this is the oracle for the golden test and the fallback
// for validation on a tenant whose registry has not been seeded.
func SeededTaskTypes() (TaskTypeRegistry, error) {
	rows, err := sopseed.TaskTypes()
	if err != nil {
		return nil, err
	}
	out := make(TaskTypeRegistry, len(rows))
	for _, r := range rows {
		out[r.Key] = FollowUpTaskTy{Key: r.Key, AnswerKind: r.AnswerKind, EngineHook: r.EngineHook, ActionKind: actionTypeFor(r.AnswerKind)}
	}
	return out, nil
}
