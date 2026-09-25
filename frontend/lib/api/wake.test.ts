import { beforeEach, describe, expect, it, vi } from "vitest";

import { resetWake, wakeAnalysisService } from "@/lib/api/wake";

const fetchMock = vi.fn();
const service = "https://ai.example.com";

beforeEach(() => {
  resetWake();
  fetchMock.mockReset();
  fetchMock.mockResolvedValue(new Response(null, { status: 200 }));
  vi.stubGlobal("fetch", fetchMock);
});

describe("wakeAnalysisService", () => {
  it("asks the AI service's health endpoint directly, from the browser", () => {
    wakeAnalysisService(service, () => 0);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    // Opaque on purpose: the response is never read, and the service does not
    // have to allow this origin for the request to reach it.
    expect(fetchMock).toHaveBeenCalledWith(
      `${service}/health`,
      expect.objectContaining({ mode: "no-cors", cache: "no-store" }),
    );
  });

  it("does nothing when no AI service address is configured", () => {
    wakeAnalysisService(undefined, () => 0);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("does not ask again while the last wake still holds", () => {
    let clock = 0;
    const now = () => clock;

    wakeAnalysisService(service, now);
    clock = 5 * 60_000; // opening the workspace, then submitting five minutes later
    wakeAnalysisService(service, now);

    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("asks again once the service may have gone back to sleep", () => {
    let clock = 0;
    const now = () => clock;

    wakeAnalysisService(service, now);
    clock = 11 * 60_000;
    wakeAnalysisService(service, now);

    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("never rejects, even when the wake fails", async () => {
    // A wake that does not land costs a slower analysis, not a failed one, so
    // nothing about it may reach the page as an error.
    fetchMock.mockRejectedValue(new TypeError("Failed to fetch"));
    await expect(wakeAnalysisService(service, () => 0)).resolves.toBeUndefined();
  });

  it("tolerates a trailing slash on the configured address", () => {
    wakeAnalysisService(`${service}/`, () => 0);
    expect(fetchMock).toHaveBeenCalledWith(`${service}/health`, expect.anything());
  });
});
