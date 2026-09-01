// Public entrypoint for the Weighing vertical on admin-web.
//
// Only the Weights read-out lives here. Planning, execution, proof capture and the verifier
// queue are phone surfaces and are deliberately not mirrored on admin-web — this is the
// oversight lens, not a second console.
export { WeighingWeightsPage } from "./weights";
// Weights analytics: the same weighing facts cut five ways. A second page rather than more cards
// on Weights, because "what does the estate weigh today" and "what is growing faster than what"
// are different questions and the second one wants the whole screen.
export { WeighingWeightsAnalyticsPage } from "./weights-analytics";
