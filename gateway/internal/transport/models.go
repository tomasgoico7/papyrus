package transport

// Localized is a single string in both supported languages.
type Localized struct {
	En string `json:"en"`
	Es string `json:"es"`
}

// LocalizedList is a list of strings in both supported languages.
type LocalizedList struct {
	En []string `json:"en"`
	Es []string `json:"es"`
}

// Suggestion is a single improvement recommendation produced by the AI service.
type Suggestion struct {
	Title    Localized `json:"title"`
	Detail   Localized `json:"detail"`
	Priority string    `json:"priority"`
}

// Analysis is the core, bilingual result returned by the AI service.
type Analysis struct {
	Score         int           `json:"score"`
	Verdict       string        `json:"verdict"`
	Summary       Localized     `json:"summary"`
	MatchedSkills LocalizedList `json:"matchedSkills"`
	MissingSkills LocalizedList `json:"missingSkills"`
	Suggestions   []Suggestion  `json:"suggestions"`
}

// AnalysisResponse is what the gateway returns to the frontend: the AI result
// augmented with the original filename, which the gateway owns.
type AnalysisResponse struct {
	Analysis
	CVFilename string `json:"cvFilename"`
}
