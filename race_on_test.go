//go:build race

package falcon

// raceEnabled reports whether the tests run under the race detector, where sync.Pool
// drops items at random on purpose, so counting allocations means nothing.
const raceEnabled = true
