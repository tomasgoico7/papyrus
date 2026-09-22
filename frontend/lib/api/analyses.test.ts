import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  awaitAnalysis,
  resetAsyncSupport,
  runAnalysis,
  submitAnalysis,
  supportsAsyncAnalysis,
} from "@/lib/api/analyses";
import { GatewayError } from "@/lib/api/gateway";
import type { AnalysisResult } from "@/lib/types";

const analysis = {
  score: 74,
  verdict: "moderate",
  summary: { en: "Decent.", es: "Razonable." },
  matchedSkills: { en: ["Go"], es: ["Go"] },
  missingSkills: { en: [], es: [] },
  suggestions: [],
  cvFilename: "cv.pdf",
} as unknown as AnalysisResult;

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function input() {
  return {
    cv: new File(["%PDF-1.4"], "cv.pdf", { type: "application/pdf" }),
    jobOffer: "A backend role that needs Go and Postgres.",
    accessToken: "token",
  };
}

const fetchMock = vi.fn();

beforeEach(() => {
  resetAsyncSupport();
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("submitAnalysis", () => {
  it("returns the result straight away when the gateway already had it", async () => {
    fetchMock.mockResolvedValueOnce(json(200, analysis));

    const submission = await submitAnalysis(input());

    expect(submission).toEqual({ kind: "ready", result: analysis });
  });

  it("returns a job id when the analysis has to run", async () => {
    fetchMock.mockResolvedValueOnce(json(202, { jobId: "job-1", status: "queued" }));

    const submission = await submitAnalysis(input());

    expect(submission).toEqual({ kind: "queued", jobId: "job-1" });
  });

  it("remembers a gateway that has no queue", async () => {
    fetchMock.mockResolvedValueOnce(json(404, {}));

    await expect(submitAnalysis(input())).rejects.toMatchObject({ code: "unsupported" });
    expect(supportsAsyncAnalysis()).toBe(false);
  });

  it("surfaces a rejected upload as its own error", async () => {
    fetchMock.mockResolvedValueOnce(
      json(415, { error: { code: "unsupported_media_type", message: "Only PDF." } }),
    );

    await expect(submitAnalysis(input())).rejects.toMatchObject({
      code: "unsupported_media_type",
    });
  });
});

describe("runAnalysis", () => {
  it("falls back to the synchronous endpoint, and stops asking after the first 404", async () => {
    fetchMock
      .mockResolvedValueOnce(json(404, {})) // POST /analyses
      .mockResolvedValueOnce(json(200, analysis)) // POST /analyze
      .mockResolvedValueOnce(json(200, analysis)); // POST /analyze, second run

    await expect(runAnalysis(input())).resolves.toEqual(analysis);
    await expect(runAnalysis(input())).resolves.toEqual(analysis);

    // Three calls, not four: the second run never retries the async endpoint.
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(String(fetchMock.mock.calls.at(-1)?.[0])).toContain("/analyze");
  });

  it("does not swallow a real failure as an unsupported gateway", async () => {
    fetchMock.mockResolvedValueOnce(
      json(429, { error: { code: "upstream_rate_limited", message: "Busy." } }),
    );

    await expect(runAnalysis(input())).rejects.toMatchObject({ code: "upstream_rate_limited" });
    // The synchronous endpoint must not be tried: the request was accepted and
    // refused, not misrouted.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});

describe("awaitAnalysis", () => {
  it("polls until the job finishes", async () => {
    vi.useFakeTimers();
    fetchMock
      .mockResolvedValueOnce(json(200, { jobId: "j", status: "queued", attempt: 0 }))
      .mockResolvedValueOnce(json(200, { jobId: "j", status: "running", attempt: 1 }))
      .mockResolvedValueOnce(json(200, { jobId: "j", status: "done", attempt: 1, result: analysis }));

    const pending = awaitAnalysis("j", "token");
    await vi.advanceTimersByTimeAsync(10_000);

    await expect(pending).resolves.toEqual(analysis);
  });

  it("reports a failed job with the reason the worker recorded", async () => {
    vi.useFakeTimers();
    fetchMock.mockResolvedValueOnce(
      json(200, {
        jobId: "j",
        status: "failed",
        attempt: 3,
        error: { code: "unreadable_cv", message: "The CV is not readable." },
      }),
    );

    const failure = await awaitAnalysis("j", "token").catch((error: unknown) => error);

    expect(failure).toBeInstanceOf(GatewayError);
    expect(failure).toMatchObject({ code: "unreadable_cv" });
  });

  it("keeps polling through a blip", async () => {
    vi.useFakeTimers();
    fetchMock
      .mockRejectedValueOnce(new TypeError("network down"))
      .mockResolvedValueOnce(json(500, {}))
      .mockResolvedValueOnce(json(200, { jobId: "j", status: "done", attempt: 1, result: analysis }));

    const pending = awaitAnalysis("j", "token");
    await vi.advanceTimersByTimeAsync(20_000);

    // A network hiccup while polling is not a failed analysis.
    await expect(pending).resolves.toEqual(analysis);
  });

  it("reports progress while the job is still going", async () => {
    vi.useFakeTimers();
    fetchMock
      .mockResolvedValueOnce(json(200, { jobId: "j", status: "running", attempt: 2 }))
      .mockResolvedValueOnce(json(200, { jobId: "j", status: "done", attempt: 2, result: analysis }));

    const seen: number[] = [];
    const pending = awaitAnalysis("j", "token", {
      onProgress: (snapshot) => seen.push(snapshot.attempt),
    });
    await vi.advanceTimersByTimeAsync(10_000);
    await pending;

    expect(seen).toEqual([2]);
  });

  it("stops when the caller walks away", async () => {
    vi.useFakeTimers();
    fetchMock.mockResolvedValue(json(200, { jobId: "j", status: "running", attempt: 1 }));

    const controller = new AbortController();
    const settled = expect(
      awaitAnalysis("j", "token", { signal: controller.signal }),
    ).rejects.toMatchObject({ name: "AbortError" });

    await vi.advanceTimersByTimeAsync(1_000);
    controller.abort();
    await settled;
  });

  it("gives up after a while, telling the caller the job is still running", async () => {
    vi.useFakeTimers();
    fetchMock.mockResolvedValue(json(200, { jobId: "j", status: "running", attempt: 1 }));

    // The assertion is attached before the clock moves: otherwise the rejection
    // lands while nothing is listening and Node flags it as unhandled.
    const settled = expect(awaitAnalysis("j", "token")).rejects.toMatchObject({
      code: "still_running",
    });
    // The result is cached either way, so asking again is cheap — which is what
    // the code tells the caller to do.
    await vi.advanceTimersByTimeAsync(5 * 60_000);
    await settled;
  });
});
