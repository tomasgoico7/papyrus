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

export interface TailoringQuestion {
  topic: string;
  question: string;
}

export interface TailoringAnswer {
  topic: string;
  answer: string;
}

export interface ExperienceItem {
  role: string;
  company: string;
  period: string;
  highlights: string[];
}

export interface EducationItem {
  degree: string;
  institution: string;
  period: string;
}

/** A CV rewritten for a specific posting, grounded in the user's real data. */
export interface TailoredCv {
  fullName: string;
  contact: string;
  headline: string;
  summary: string;
  experience: ExperienceItem[];
  skills: string[];
  education: EducationItem[];
}

/** A persisted analysis as surfaced to the dashboard history. */
export interface AnalysisRecord {
  id: string;
  jobTitle: string | null;
  jobOffer: string;
  cvFilename: string | null;
  cvStoragePath: string | null;
  score: number;
  verdict: Verdict;
  summary: Localized;
  matchedSkills: LocalizedList;
  missingSkills: LocalizedList;
  suggestions: Suggestion[];
  createdAt: string;
  shareToken: string | null;
  shareExpiresAt: string | null;
}

export interface AuthenticatedUser {
  id: string;
  email: string;
  fullName: string | null;
  avatarUrl: string | null;
}
