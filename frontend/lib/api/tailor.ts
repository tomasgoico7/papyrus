import { GatewayError } from "@/lib/api/gateway";
import { env } from "@/lib/env";
import type { Locale } from "@/lib/i18n/config";
import type { TailoredCv, TailoringAnswer, TailoringQuestion } from "@/lib/types";

async function failFrom(response: Response): Promise<GatewayError> {
  const envelope = (await response.json().catch(() => null)) as {
    error?: { code?: string; message?: string };
  } | null;
  return new GatewayError(
    envelope?.error?.message ?? "The request couldn't be completed.",
    envelope?.error?.code ?? "unknown_error",
    response.status,
  );
}

interface QuestionsInput {
  cv: File;
  jobOffer: string;
  jobTitle?: string;
  locale: Locale;
  accessToken: string;
}

export interface TailoringQuestions {
  questions: TailoringQuestion[];
  cvText: string;
}

export async function requestTailoringQuestions({
  cv,
  jobOffer,
  jobTitle,
  locale,
  accessToken,
}: QuestionsInput): Promise<TailoringQuestions> {
  const form = new FormData();
  form.append("cv", cv);
  form.append("jobOffer", jobOffer);
  if (jobTitle) form.append("jobTitle", jobTitle);
  form.append("locale", locale);

  let response: Response;
  try {
    response = await fetch(`${env.NEXT_PUBLIC_GATEWAY_URL}/tailor/questions`, {
      method: "POST",
      headers: { Authorization: `Bearer ${accessToken}` },
      body: form,
    });
  } catch {
    throw new GatewayError(
      "Couldn't reach the service. Check your connection and try again.",
      "network_error",
      0,
    );
  }

  if (!response.ok) throw await failFrom(response);
  return (await response.json()) as TailoringQuestions;
}

interface GenerateInput {
  cvText: string;
  jobOffer: string;
  jobTitle?: string;
  answers: TailoringAnswer[];
  extra?: string;
  accessToken: string;
}

export async function generateTailoredCv({
  cvText,
  jobOffer,
  jobTitle,
  answers,
  extra,
  accessToken,
}: GenerateInput): Promise<TailoredCv> {
  let response: Response;
  try {
    response = await fetch(`${env.NEXT_PUBLIC_GATEWAY_URL}/tailor/generate`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${accessToken}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ cvText, jobOffer, jobTitle, answers, extra }),
    });
  } catch {
    throw new GatewayError(
      "Couldn't reach the service. Check your connection and try again.",
      "network_error",
      0,
    );
  }

  if (!response.ok) throw await failFrom(response);
  return (await response.json()) as TailoredCv;
}
