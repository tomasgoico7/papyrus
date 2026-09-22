// Package ratelimit decides whether a caller may proceed.
//
// The shape is a token bucket: a steady refill with room for a burst, which is
// what a person clicking a button actually looks like. Where it runs from is the
// interesting part — in one process it is a map, across replicas it has to be
// somewhere all of them can see.
package ratelimit

import "context"

// Limiter reports whether one more request from key is allowed right now.
//
// An error means the decision could not be made, not that it was "no". A limiter
// that cannot answer must never be the reason a request fails: the caller is
// expected to fall back to something that can.
type Limiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}
