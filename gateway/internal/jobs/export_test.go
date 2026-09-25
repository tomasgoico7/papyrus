package jobs

// The queue's statements, exposed to the package's tests so the ones that check
// query plans explain the real SQL rather than copies that could drift.
const (
	ClaimSQL         = claimSQL
	MeasureSQL       = measureSQL
	FailAbandonedSQL = failAbandonedSQL
	PurgeFinishedSQL = purgeFinishedSQL
)
