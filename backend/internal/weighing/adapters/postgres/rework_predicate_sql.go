package postgres

import "fmt"

func shedReworkOutstandingPredicate(alias string) string {
	return fmt.Sprintf(`(
  EXISTS (SELECT 1 FROM weighing_observations wo
           WHERE wo.tenant_id=%[1]s.tenant_id AND wo.campaign_shed_id=%[1]s.campaign_shed_id
             AND wo.submitted_at IS NOT NULL AND wo.verification_status='rework')
  OR EXISTS (SELECT 1 FROM weighing_shed_observations wso
              WHERE wso.tenant_id=%[1]s.tenant_id AND wso.campaign_shed_id=%[1]s.campaign_shed_id
                AND wso.verification_status='rework')
)`, alias)
}
