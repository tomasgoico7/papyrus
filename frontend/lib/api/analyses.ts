import { env } from "@/lib/env";
import {
  GatewayError,
  buildAnalysisForm,
  readGatewayError,
  requestAnalysis,
} from "@/lib/api/gateway";
import type { AnalyzeInput } from "@/lib/api/gateway";
import type { AnalysisResult } from "@/lib/types";
import { wakeAnalysisService } from "@/lib/api/wake";

/** What the gateway does with a submitted analysis. */
export type Submission =
  | { kind: "ready"; result: AnalysisResult }
  | { kind: "queued"; jobId: string };

/** Where a queued analysis has got to. */
export type JobSnapshot =
  | { status: "queued" | "running"; attempt: number }
  | { status: "done"; result: AnalysisResult }
  | { status: "failed"; code: string; message: string };

/**
 * Whether this gateway serves the asynchronous endpoints.
 *
 * The backend already decides this from its own configuration; asking it once
 * and remembering keeps the answer in one place. A build-time flag would be a
 * second copy of the same decision, and the two would drift.
 */
let asyncSupported: boolean | null = null;

export function supportsAsyncAnalysis(): boolean | null {
  return asyncSupported;
}

/** Test seam: the cached capability is module state, and tests need it reset. */
export function resetAsyncSupport(): void {
  asyncSupported = null;
}

/**
 * Submits an analysis without waiting for the model.
 *
 * Returns `ready` when the gateway already had the answer — a cache hit needs no
 * job — and `queued` otherwise. Throws `unsupported` on a gateway with no queue,
 * so the caller can fall back to the synchronous endpoint.
 */
export async function submitAnalysis({
  cv,
  jobOffer,
  jobTitle,
  accessToken,
}: AnalyzeInput): Promise<Submission> {
  let response: Response;
  try {
    response = await fetch(`${env.NEXT_PUBLIC_GATEWAY_URL}/analyses`, {
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

  if (response.status === 404) {
    asyncSupported = false;
    throw new GatewayError("This gateway has no queue.", "unsupported", 404);
  }
  if (!response.ok) {
    throw await readGatewayError(response);
  }

  asyncSupported = true;

  if (response.status === 200) {
    return { kind: "ready", result: (await response.json()) as AnalysisResult };
  }

  const accepted = (await response.json()) as { jobId: string };
  return { kind: "queued", jobId: accepted.jobId };
}

/** Reads a queued analysis once. */
export async function fetchJob(
  jobId: string,
  accessToken: string,
  signal?: AbortSignal,
): Promise<JobSnapshot> {
  let response: Response;
  try {
    response = await fetch(`${env.NEXT_PUBLIC_GATEWAY_URL}/analyses/${jobId}`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      signal,
    });
  } catch (error) {
    if (signal?.aborted) throw error;
    throw new GatewayError(
      "Couldn't reach the analysis service. Check your connection and try again.",
      "network_error",
      0,
    );
  }

  if (!response.ok) {
    throw await readGatewayError(response);
  }

  const body = (await response.json()) as {
    status: JobSnapshot["status"];
    attempt: number;
    result?: AnalysisResult;
    error?: { code: string; message: string };
  };

  switch (body.status) {
    case "done":
      if (!body.result) {
        throw new GatewayError("The analysis finished without a result.", "analysis_failed", 500);
      }
      return { status: "done", result: body.result };
    case "failed":
      return {
        status: "failed",
        code: body.error?.code ?? "analysis_failed",
        message: body.error?.message ?? "The analysis could not be completed.",
      };
    default:
      return { status: body.status, attempt: body.attempt };
  }
}

const FIRST_DELAY_MS = 800;
const MAX_DELAY_MS = 4_000;
const GROWTH = 1.5;
/**
 * How long to keep asking. The worker retries a failing job with its own
 * backoff, so a job can legitimately outlive this; giving up here abandons the
 * polling, not the job.
 */
const DEADLINE_MS = 4 * 60_000;

export interface AwaitOptions {
  signal?: AbortSignal;
  onProgress?: (snapshot: Extract<JobSnapshot, { status: "queued" | "running" }>) => void;
}

/**
 * Polls a queued analysis until it finishes.
 *
 * The interval grows rather than staying fixed: a job that is going to take
 * thirty seconds should not be asked about forty times. Errors while polling are
 * tolerated — a blip should not lose an analysis that is still running — but a
 * failed job is returned as a `GatewayError`, because that one is final.
 */
export async function awaitAnalysis(
  jobId: string,
  accessToken: string,
  { signal, onProgress }: AwaitOptions = {},
): Promise<AnalysisResult> {
  const deadline = Date.now() + DEADLINE_MS;
  let delay = FIRST_DELAY_MS;

  for (;;) {
    if (signal?.aborted) {
      throw new DOMException("Polling aborted", "AbortError");
    }
    if (Date.now() > deadline) {
      // The job is still running and its result will be cached, so asking again
      // is cheap — which is what the message tells the user to do.
      throw new GatewayError(
        "The analysis is taking longer than expected. Try again in a moment.",
        "still_running",
        0,
      );
    }

    let snapshot: JobSnapshot;
    try {
      snapshot = await fetchJob(jobId, accessToken, signal);
    } catch (error) {
      if (signal?.aborted) throw error;
      // A transient failure while polling is not a failed analysis.
      await wait(delay, signal);
      delay = Math.min(delay * GROWTH, MAX_DELAY_MS);
      continue;
    }

    if (snapshot.status === "done") {
      return snapshot.result;
    }
    if (snapshot.status === "failed") {
      throw new GatewayError(snapshot.message, snapshot.code, 200);
    }

    onProgress?.(snapshot);
    await wait(delay, signal);
    delay = Math.min(delay * GROWTH, MAX_DELAY_MS);
  }
}

function wait(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);

    function onAbort() {
      clearTimeout(timer);
      reject(new DOMException("Polling aborted", "AbortError"));
    }

    if (signal?.aborted) {
      onAbort();
      return;
    }
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

/**
 * Runs an analysis by whichever route the gateway offers.
 *
 * The asynchronous path is tried first and remembered; a gateway with no queue
 * answers 404 once and every later call goes straight to the synchronous
 * endpoint. That keeps one deployable frontend working against both, instead of
 * a build-time flag that has to be kept in step with the backend's own config.
 */
export async function runAnalysis(
  input: AnalyzeInput,
  options: AwaitOptions = {},
): Promise<AnalysisResult> {
  // Again at submit time, in case the workspace has been open long enough for
  // the service to fall back asleep. Inside the ten minute window it is a no-op.
  void wakeAnalysisService();

  if (asyncSupported !== false) {
    try {
      const submission = await submitAnalysis(input);
      return submission.kind === "ready"
        ? submission.result
        : await awaitAnalysis(submission.jobId, input.accessToken, options);
    } catch (error) {
      if (!(error instanceof GatewayError) || error.code !== "unsupported") {
        throw error;
      }
      // This gateway has no queue. Fall through and wait for the model.
    }
  }

  return requestAnalysis(input);
}
