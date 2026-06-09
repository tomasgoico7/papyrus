import type { SupabaseClient } from "@supabase/supabase-js";

import type {
  AnalysisRecord,
  AnalysisResult,
  Localized,
  LocalizedList,
  Suggestion,
} from "@/lib/types";

interface AnalysisRow {
  id: string;
  job_title: string | null;
  cv_filename: string | null;
  score: number;
  verdict: AnalysisRecord["verdict"];
  summary: Localized;
  matched_skills: LocalizedList;
  missing_skills: LocalizedList;
  suggestions: Suggestion[];
  created_at: string;
  cvs: { storage_path: string | null } | null;
}

const SELECT =
  "id, job_title, cv_filename, score, verdict, summary, matched_skills, missing_skills, suggestions, created_at, cvs ( storage_path )";

function toRecord(row: AnalysisRow): AnalysisRecord {
  return {
    id: row.id,
    jobTitle: row.job_title,
    cvFilename: row.cv_filename,
    cvStoragePath: row.cvs?.storage_path ?? null,
    score: row.score,
    verdict: row.verdict,
    summary: row.summary,
    matchedSkills: row.matched_skills,
    missingSkills: row.missing_skills,
    suggestions: row.suggestions,
    createdAt: row.created_at,
  };
}

export async function listAnalyses(
  supabase: SupabaseClient,
  userId: string,
): Promise<AnalysisRecord[]> {
  const { data, error } = await supabase
    .from("analyses")
    .select(SELECT)
    .eq("user_id", userId)
    .order("created_at", { ascending: false })
    .limit(50);

  if (error) {
    throw new Error(`Failed to load analyses: ${error.message}`);
  }

  return (data as unknown as AnalysisRow[]).map(toRecord);
}

export interface SaveAnalysisInput {
  result: AnalysisResult;
  jobOffer: string;
  jobTitle?: string;
  cvId?: string;
}

export async function saveAnalysis(
  supabase: SupabaseClient,
  userId: string,
  { result, jobOffer, jobTitle, cvId }: SaveAnalysisInput,
): Promise<AnalysisRecord> {
  const { data, error } = await supabase
    .from("analyses")
    .insert({
      user_id: userId,
      cv_id: cvId ?? null,
      job_title: jobTitle ?? null,
      job_offer: jobOffer,
      cv_filename: result.cvFilename,
      score: result.score,
      verdict: result.verdict,
      summary: result.summary,
      matched_skills: result.matchedSkills,
      missing_skills: result.missingSkills,
      suggestions: result.suggestions,
    })
    .select(SELECT)
    .single();

  if (error) {
    throw new Error(`Failed to save analysis: ${error.message}`);
  }

  return toRecord(data as unknown as AnalysisRow);
}

export async function deleteAnalysis(
  supabase: SupabaseClient,
  id: string,
): Promise<void> {
  const { error } = await supabase.from("analyses").delete().eq("id", id);
  if (error) {
    throw new Error(`Failed to delete analysis: ${error.message}`);
  }
}
