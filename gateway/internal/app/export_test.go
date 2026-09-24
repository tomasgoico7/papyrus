package app

import "time"

// ReadinessTimeout exposes the probe's budget so the package's tests can assert
// the shared client is sized to cover it.
const ReadinessTimeout time.Duration = readinessTimeout
