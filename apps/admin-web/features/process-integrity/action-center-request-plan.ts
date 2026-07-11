import type {
  ProcessIntegritySeverity,
  WorkState,
} from "../../lib/api/server";

type ActionCenterRequestPlanInput = {
  stateFilter: WorkState | "all";
  severityFilter: ProcessIntegritySeverity | "all";
  requestedBoardPage: {
    pageSize: number;
    offset: number;
  };
  parkId?: string;
  asOf?: string;
};

export type ActionCenterRequestPlan = {
  actionCenter: {
    parkId?: string;
    asOf?: string;
    workState?: WorkState;
    severity?: ProcessIntegritySeverity;
    limit: number;
    offset: number;
  };
  verificationQueue: {
    parkId?: string;
    limit: number;
  };
};

export function actionCenterRequestPlan(input: ActionCenterRequestPlanInput): ActionCenterRequestPlan {
  return {
    actionCenter: {
      parkId: input.parkId,
      asOf: input.asOf,
      workState: input.stateFilter === "all" ? undefined : input.stateFilter,
      severity: input.severityFilter === "all" ? undefined : input.severityFilter,
      limit: input.requestedBoardPage.pageSize,
      offset: input.requestedBoardPage.offset,
    },
    verificationQueue: {
      parkId: input.parkId,
      limit: 200,
    },
  };
}
