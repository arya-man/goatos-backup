package domain

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// UnknownFollowUpKeys names, by path, every key of form_dsl.follow_up the document does not define.
// encoding/json drops an unknown key silently, so a misspelt "vidoe" in a step's proof would publish
// a step that needs no proof at all. The allowed keys are read from the json tags of the typed DSL,
// so a field added to FollowUpStep is accepted without a second list to keep in step. An absent or
// non-object section returns nothing (ParseFollowUp reports those).
func UnknownFollowUpKeys(formDSL map[string]any) []string {
	root, ok := formDSL["follow_up"].(map[string]any)
	if !ok {
		return nil
	}
	var out []string
	check := func(path string, v any, t reflect.Type) map[string]any {
		obj, isObj := v.(map[string]any)
		if !isObj {
			return nil
		}
		allowed := jsonKeys(t)
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !allowed[k] {
				out = append(out, path+"."+k)
			}
		}
		return obj
	}
	check("follow_up", root, reflect.TypeOf(FollowUpDSL{}))
	tracks, _ := root["tracks"].([]any)
	for ti, tr := range tracks {
		tp := fmt.Sprintf("follow_up.tracks.%d", ti)
		track := check(tp, tr, reflect.TypeOf(FollowUpTrack{}))
		if track == nil {
			continue
		}
		steps, _ := track["steps"].([]any)
		for si, st := range steps {
			sp := fmt.Sprintf("%s.steps.%d", tp, si)
			step := check(sp, st, reflect.TypeOf(FollowUpStep{}))
			if step == nil {
				continue
			}
			if proof, ok := step["proof"]; ok && proof != nil {
				check(sp+".proof", proof, reflect.TypeOf(FollowUpProof{}))
			}
			if schedule, ok := step["schedule"]; ok && schedule != nil {
				check(sp+".schedule", schedule, reflect.TypeOf(FollowUpSchedule{}))
			}
		}
	}
	return out
}

func jsonKeys(t reflect.Type) map[string]bool {
	out := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			out[name] = true
		}
	}
	return out
}
