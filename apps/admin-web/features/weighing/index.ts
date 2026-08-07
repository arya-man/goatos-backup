// Public entrypoint for the Weighing vertical on admin-web.
//
// Only the Weights read-out lives here. Planning, execution, proof capture and the verifier
// queue are phone surfaces and are deliberately not mirrored on admin-web — this is the
// oversight lens, not a second console.
export { WeighingWeightsPage } from "./weights";
