package domain

import "sort"

// PageSubtasks is for a source whose row has a SMALL, bounded list of units (an engine
// workflow's steps, a toxin round's procedure): it sorts the row's WHOLE list on its key -- worst
// first -- and returns the page after afterKey. Total is always the whole list.
func PageSubtasks(all []Subtask, afterKey string, limit int) SubtaskPage {
	sort.Slice(all, func(i, j int) bool { return lessSubtaskKey(all[i].Key, all[j].Key) })
	if limit <= 0 {
		limit = 10
	}
	page := SubtaskPage{Subtasks: []Subtask{}, Total: len(all)}
	for _, st := range all {
		if afterKey != "" && !lessSubtaskKey(afterKey, st.Key) {
			continue
		}
		if len(page.Subtasks) == limit {
			page.NextCursor = page.Subtasks[len(page.Subtasks)-1].Key
			break
		}
		page.Subtasks = append(page.Subtasks, st)
	}
	return page
}

// lessSubtaskKey orders on the rank first, then the id -- the same order ParseSubtaskKey reads.
func lessSubtaskKey(a, b string) bool {
	ra, ia, _ := ParseSubtaskKey(a)
	rb, ib, _ := ParseSubtaskKey(b)
	if ra != rb {
		return ra < rb
	}
	return ia < ib
}
