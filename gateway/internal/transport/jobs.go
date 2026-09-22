package transport

// JobAccepted is the answer to a submitted analysis that still has to run.
type JobAccepted struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
}

// JobError explains why a job will not produce a result. It carries the same
// shape as the error envelope, but nested: the request to read the job
// succeeded, so the failure belongs in the body rather than in the status.
type JobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// JobStatus is what polling a job returns. Exactly one of Result and Error is
// set, and only once the job has finished.
type JobStatus struct {
	JobID   string            `json:"jobId"`
	Status  string            `json:"status"`
	Attempt int               `json:"attempt"`
	Result  *AnalysisResponse `json:"result,omitempty"`
	Error   *JobError         `json:"error,omitempty"`
}
