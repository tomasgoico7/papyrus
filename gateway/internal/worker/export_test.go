package worker

// Defaults exposes the filled-in configuration to the package's tests. The
// defaults are part of the behaviour — one of them is derived from three others
// — so they are worth asserting on directly rather than through a running loop.
func Defaults(c Config) Config { return c.withDefaults() }
