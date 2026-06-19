import type { SupabaseClient } from "@supabase/supabase-js";

import type { AnalysisViewData } from "@/components/analysis/analysis-view";
import type {
  AnalysisRecord,
  AnalysisResult,
  Localized,
  LocalizedList,
  Suggestion,
  TailoredCv,
} from "@/lib/types";

interface AnalysisRow {
  id: string;
  job_title: string | null;
  job_offer: string;
  cv_filename: string | null;
  score: number;
  verdict: AnalysisRecord["verdict"];
  summary: Localized;
  matched_skills: LocalizedList;
  missing_skills: LocalizedList;
  suggestions: Suggestion[];
  created_at: string;
  share_token: string | null;
  share_expires_at: string | null;
  tailored_cv: TailoredCv | null;
  cvs: { storage_path: string | null } | null;
}

const SELECT =
  "id, job_title, job_offer, cv_filename, score, verdict, summary, matched_skills, missing_skills, suggestions, created_at, share_token, share_expires_at, tailored_cv, cvs ( storage_path )";

function toRecord(row: AnalysisRow): AnalysisRecord {
  return {
    id: row.id,
    jobTitle: row.job_title,
    jobOffer: row.job_offer,
    cvFilename: row.cv_filename,
    cvStoragePath: row.cvs?.storage_path ?? null,
    score: row.score,
    verdict: row.verdict,
    summary: row.summary,
    matchedSkills: row.matched_skills,
    missingSkills: row.missing_skills,
    suggestions: row.suggestions,
    createdAt: row.created_at,
    shareToken: row.share_token,
    shareExpiresAt: row.share_expires_at,
    tailoredCv: row.tailored_cv ?? null,
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

export async function saveTailoredCv(
  supabase: SupabaseClient,
  analysisId: string,
  tailoredCv: TailoredCv,
): Promise<void> {
  const { error } = await supabase
    .from("analyses")
    .update({ tailored_cv: tailoredCv })
    .eq("id", analysisId);
  if (error) {
    throw new Error(`Failed to save tailored CV: ${error.message}`);
  }
}

export interface ShareInfo {
  token: string;
  expiresAt: string;
}

export async function enableSharing(
  supabase: SupabaseClient,
  analysisId: string,
  days: number,
): Promise<ShareInfo> {
  const token = crypto.randomUUID();
  const expiresAt = new Date(Date.now() + days * 86_400_000).toISOString();

  const { error } = await supabase
    .from("analyses")
    .update({ share_token: token, share_expires_at: expiresAt })
    .eq("id", analysisId);
  if (error) {
    throw new Error(`Failed to share analysis: ${error.message}`);
  }

  return { token, expiresAt };
}

export async function disableSharing(
  supabase: SupabaseClient,
  analysisId: string,
): Promise<void> {
  const { error } = await supabase
    .from("analyses")
    .update({ share_token: null, share_expires_at: null })
    .eq("id", analysisId);
  if (error) {
    throw new Error(`Failed to stop sharing: ${error.message}`);
  }
}

interface SharedRow {
  id: string;
  job_title: string | null;
  score: number;
  verdict: AnalysisRecord["verdict"];
  summary: Localized;
  matched_skills: LocalizedList;
  missing_skills: LocalizedList;
  suggestions: Suggestion[];
  created_at: string;
}

/** Reads a publicly shared analysis by token; null when missing or expired. */
export async function getSharedAnalysis(
  supabase: SupabaseClient,
  token: string,
): Promise<AnalysisViewData | null> {
  const { data, error } = await supabase
    .rpc("get_shared_analysis", { token })
    .maybeSingle<SharedRow>();
  if (error || !data) {
    return null;
  }

  return {
    jobTitle: data.job_title,
    score: data.score,
    verdict: data.verdict,
    summary: data.summary,
    matchedSkills: data.matched_skills,
    missingSkills: data.missing_skills,
    suggestions: data.suggestions,
    createdAt: data.created_at,
  };
}
