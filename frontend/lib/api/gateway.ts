import { env } from "@/lib/env";
import { MAX_UPLOAD_MB } from "@/lib/constants";
import type { Dictionary } from "@/lib/i18n/dictionaries";
import type { AnalysisResult } from "@/lib/types";

export interface AnalyzeInput {
  cv: File;
  jobOffer: string;
  jobTitle?: string;
  accessToken: string;
}

export class GatewayError extends Error {
  constructor(
    message: string,
    readonly code: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "GatewayError";
  }
}

/** The multipart body both analyze endpoints expect. */
export function buildAnalysisForm({
  cv,
  jobOffer,
  jobTitle,
}: Omit<AnalyzeInput, "accessToken">): FormData {
  const form = new FormData();
  form.append("cv", cv);
  form.append("jobOffer", jobOffer);
  if (jobTitle) {
    form.append("jobTitle", jobTitle);
  }
  return form;
}

/** Turns the shared error envelope into a GatewayError. */
export async function readGatewayError(response: Response): Promise<GatewayError> {
  const envelope = (await response.json().catch(() => null)) as {
    error?: { code?: string; message?: string };
  } | null;

  return new GatewayError(
    envelope?.error?.message ?? "The analysis couldn't be completed.",
    envelope?.error?.code ?? "unknown_error",
    response.status,
  );
}

export async function requestAnalysis({
  cv,
  jobOffer,
  jobTitle,
  accessToken,
}: AnalyzeInput): Promise<AnalysisResult> {
  let response: Response;
  try {
    response = await fetch(`${env.NEXT_PUBLIC_GATEWAY_URL}/analyze`, {
      method: "POST",
      headers: { Authorization: `Bearer ${accessToken}` },
      body: buildAnalysisForm({ cv, jobOffer, jobTitle }),
    });
  } catch {
    throw new GatewayError(
      "Couldn't reach the analysis service. Check your connection and try again.",
      "network_error",
      0,
    );
  }

  if (!response.ok) {
    throw await readGatewayError(response);
  }

  return (await response.json()) as AnalysisResult;
}

const ERROR_CODE_KEYS: Record<string, keyof Dictionary["result"]["errors"]> = {
  invalid_request: "invalidRequest",
  invalid_job_offer: "invalidJobOffer",
  cv_required: "cvRequired",
  unsupported_media_type: "unsupportedMediaType",
  payload_too_large: "payloadTooLarge",
  upload_error: "uploadError",
  unreadable_cv: "unreadableCv",
  analysis_failed: "analysisFailed",
  upstream_timeout: "upstreamTimeout",
  upstream_unavailable: "upstreamUnavailable",
  upstream_rate_limited: "upstreamRateLimited",
  still_running: "stillRunning",
  queue_unavailable: "queueUnavailable",
  // A worker died mid-analysis and the job ran out of attempts. Retrying is
  // the right move and usually works, so it must not fall through to the
  // generic message the way an unmapped code would.
  worker_lost: "workerLost",
  rate_limited: "rateLimited",
  // The gateway sends this when the AI service answers with something that is
  // not our error envelope — a proxy page, usually. Without it here the user
  // got the generic message for a condition that retrying often fixes.
  ai_service_error: "aiServiceError",
  network_error: "networkError",
};

export function localizeGatewayError(
  error: GatewayError,
  t: Dictionary,
  maxUploadMb: number = MAX_UPLOAD_MB,
): string {
  if (error.code === "unauthorized") return t.result.sessionExpired;

  const key = ERROR_CODE_KEYS[error.code];
  if (!key) return t.result.errorGeneric;

  return t.result.errors[key].replace("{mb}", String(maxUploadMb));
}
