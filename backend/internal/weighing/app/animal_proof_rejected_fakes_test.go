package app

import "context"

// AnimalProofWasRejected keeps the standalone ports.Repository fakes in this package compiling
// after the rework-proof reuse guard was added. These fakes exist to exercise unrelated paths
// (park scope, alerts), so the honest default is "this proof was not rejected".
func (r *parkScopeCheckRepo) AnimalProofWasRejected(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (r *scenarioRepo) AnimalProofWasRejected(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (r *multiParkScenarioRepo) AnimalProofWasRejected(context.Context, string, string, string) (bool, error) {
	return false, nil
}
