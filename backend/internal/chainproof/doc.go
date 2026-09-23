// Package chainproof holds the tests that prove a registered domain-event CHAIN end to end:
// the producer driven through its real service path, the envelope its own transaction committed,
// the bus the production wiring registered, and the consumer's resulting state.
//
// It exists as its own package for a plain reason: a chain test must import BOTH ends, and a
// producer's package frequently already imports the wiring that registers the consumer -- putting
// the test in either end's package is an import cycle. A neutral package that nothing imports can
// reach both.
//
// Every test here must go red under BOTH mutations, and must say which assertion catches which:
// deleting the producer's emission, and making the consumer's handler a no-op. A test that only
// catches one direction is half a chain and is marked as such in its own comment.
//
// Fixtures seed only external inputs -- tenant, animal, location, workforce, authored config.
// Obligations, completions, workflows, projections and notifications are produced by the same
// service, consumer or sweeper production uses.
package chainproof
