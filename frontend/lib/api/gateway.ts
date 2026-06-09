import { env } from "@/lib/env";
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
