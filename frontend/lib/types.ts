export type Priority = "high" | "medium" | "low";
export type Verdict = "strong" | "moderate" | "weak";

/** A string available in both supported languages. */
export interface Localized {
  en: string;
  es: string;
}

/** A list of strings available in both supported languages. */
export interface LocalizedList {
  en: string[];
  es: string[];
}

export interface Suggestion {
  title: Localized;
  detail: Localized;
  priority: Priority;
}

/** Shape returned by the gateway's `POST /analyze`. */
export interface AnalysisResult {
  score: number;
  verdict: Verdict;
  summary: Localized;
  matchedSkills: LocalizedList;
  missingSkills: LocalizedList;
  suggestions: Suggestion[];
  cvFilename: string;
}

/** A persisted analysis as surfaced to the dashboard history. */
export interface AnalysisRecord {
  id: string;
  jobTitle: string | null;
  cvFilename: string | null;
  cvStoragePath: string | null;
  score: number;
  verdict: Verdict;
  summary: Localized;
  matchedSkills: LocalizedList;
  missingSkills: LocalizedList;
  suggestions: Suggestion[];
  createdAt: string;
}

export interface AuthenticatedUser {
  id: string;
  email: string;
  fullName: string | null;
  avatarUrl: string | null;
}
