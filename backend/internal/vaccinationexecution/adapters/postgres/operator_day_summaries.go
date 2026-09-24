package postgres

import (
	"fmt"
	"sort"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// addOperatorDaySummaries unions the same dated memberships the scan route opens.
// Animal IDs remain internal; only bounded per-pen/day totals leave the backend.
func addOperatorDaySummaries(summaries map[string]*domain.ShedCardSummary, asOf time.Time) {
	today := asOf.In(biztime.DefaultLocation()).Format("2006-01-02")
	keys := make([]string, 0, len(summaries))
	for key := range summaries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	type pen struct {
		owner   *domain.ShedCardSummary
		members []domain.ExecutionRosterMembership
	}
	pens := map[string]*pen{}
	for _, key := range keys {
		summary := summaries[key]
		penKey := domain.BuildAssignmentCardID(summary.ShedID, domain.StringOrEmpty(summary.PartitionLabel), "", "", "", "")
		group := pens[penKey]
		if group == nil {
			group = &pen{owner: summary}
			pens[penKey] = group
		}
		group.members = append(group.members, summary.RosterMemberships...)
	}
	for _, group := range pens {
		dates := map[string]bool{today: true}
		for _, member := range group.members {
			if member.PlannedDate != "" {
				dates[member.PlannedDate] = true
			}
		}
		ordered := make([]string, 0, len(dates))
		for date := range dates {
			ordered = append(ordered, date)
		}
		sort.Strings(ordered)
		for _, date := range ordered {
			selected := []domain.ExecutionRosterMembership{}
			for _, member := range group.members {
				if member.PlannedDate == date || (date == today && member.PlannedDate < today && member.IncludeWhenOverdue) {
					selected = append(selected, member)
				}
			}
			if len(selected) == 0 {
				continue
			}
			group.owner.OperatorDaySummaries = append(group.owner.OperatorDaySummaries, summarizeOperatorDay(date, selected))
		}
	}
	// IDs are not needed after union and must never survive into a cached public projection.
	for _, summary := range summaries {
		for i := range summary.RosterMemberships {
			summary.RosterMemberships[i].Animals = nil
		}
	}
}

func summarizeOperatorDay(date string, members []domain.ExecutionRosterMembership) domain.OperatorDaySummary {
	result := domain.OperatorDaySummary{BusinessDate: date, VaccineGroups: []domain.VaccineGroupSummary{}}
	animals := map[string]domain.MembershipAnimal{}
	vaccines := map[string]domain.VaccineGroupSummary{}
	overdue, review := false, false
	for _, member := range members {
		result.NeedsRedo = result.NeedsRedo || member.NeedsRedo || member.Status == domain.WorkStateRejected
		overdue = overdue || member.Status == domain.WorkStateOverdue
		review = review || member.Status == domain.WorkStateVerificationPending || member.Status == domain.WorkStateProofPending
		for _, animal := range member.Animals {
			if previous, ok := animals[animal.ID]; ok {
				animal.Open = animal.Open || previous.Open
				animal.Done = animal.Done || previous.Done
				animal.Accepted = animal.Accepted && previous.Accepted
			}
			animals[animal.ID] = animal
		}
		for _, vaccine := range member.VaccineGroups {
			if previous, ok := vaccines[vaccine.Label]; ok {
				vaccine.DoseCount += previous.DoseCount
				vaccine.Full = vaccine.Full && previous.Full
			}
			vaccines[vaccine.Label] = vaccine
		}
	}
	result.TargetCount = len(animals)
	for _, animal := range animals {
		if animal.Open {
			result.OpenCount++
		} else if animal.Done {
			result.DoneCount++
		}
		if animal.Accepted {
			result.AcceptedCount++
		}
	}
	switch {
	case result.NeedsRedo:
		result.Status = domain.WorkStateRejected
	case overdue:
		result.Status = domain.WorkStateOverdue
	case review && result.OpenCount == 0:
		result.Status = domain.WorkStateVerificationPending
	case result.OpenCount == 0 && result.TargetCount > 0:
		result.Status = domain.WorkStateCompleted
	default:
		result.Status = domain.WorkStateDue
	}
	labels := make([]string, 0, len(vaccines))
	for label := range vaccines {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		vaccine := vaccines[label]
		vaccine.CountLabel = fmt.Sprintf("%d doses", vaccine.DoseCount)
		result.VaccineGroups = append(result.VaccineGroups, vaccine)
	}
	return result
}
