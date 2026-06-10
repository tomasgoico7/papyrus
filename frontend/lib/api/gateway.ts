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

export async function requestAnalysis({
  cv,
  jobOffer,
  jobTitle,
  accessToken,
}: AnalyzeInput): Promise<AnalysisResult> {
  const form = new FormData();
  form.append("cv", cv);
  form.append("jobOffer", jobOffer);
  if (jobTitle) {
    form.append("jobTitle", jobTitle);
  }

  let response: Response;
  try {
    response = await fetch(`${env.NEXT_PUBLIC_GATEWAY_URL}/analyze`, {
      method: "POST",
      headers: { Authorization: `Bearer ${accessToken}` },
      body: form,
    });
  } catch {
    throw new GatewayError(
      "Couldn't reach the analysis service. Check your connection and try again.",
      "network_error",
      0,
    );
  }

  if (!response.ok) {
    const envelope = (await response.json().catch(() => null)) as {
      error?: { code?: string; message?: string };
    } | null;
    throw new GatewayError(
      envelope?.error?.message ?? "The analysis couldn't be completed.",
      envelope?.error?.code ?? "unknown_error",
      response.status,
    );
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
