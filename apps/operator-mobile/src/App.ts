import { deriveBootstrapScreen, type BootstrapScreenState } from "./features/bootstrap/state.js";
import { buildTaskListModel, type TaskListItemModel } from "./features/tasks/viewModels.js";
import type { BootstrapResponse, TaskListResponse } from "./shared/api/client.js";

export type OperatorAppModel = {
  bootstrap: BootstrapScreenState;
  tasks: TaskListItemModel[];
};

export function createOperatorAppModel(input: {
  bootstrap: BootstrapResponse;
  tasks: TaskListResponse;
  appVersion: string;
}): OperatorAppModel {
  return {
    bootstrap: deriveBootstrapScreen(input.bootstrap, input.appVersion),
    tasks: buildTaskListModel(input.tasks),
  };
}
