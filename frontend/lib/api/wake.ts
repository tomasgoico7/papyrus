import { env } from "@/lib/env";

/**
 * How long one wake is trusted to hold. The platform parks the AI service after
 * about fifteen idle minutes, so asking again inside that window is a request
 * that changes nothing.
 */
const WAKE_TTL_MS = 10 * 60_000;

let lastWake = Number.NEGATIVE_INFINITY;

/**
 * Wakes the AI service from the browser, ahead of the analysis that will need it.
 *
 * The gateway cannot do this for itself. A request from the gateway to a parked
 * AI service is refused at the platform's edge with a 429 and starts nothing;
 * the same request from outside the platform is held while the instance starts
 * and answered in about thirty seconds. That was measured with the gateway's own
 * HTTP client, same protocol, from a residential address — where the request
 * came from was the only thing that differed.
 *
 * So the browser does it: when someone opens the workspace, and again when they
 * submit. By the time a CV is uploaded and an offer pasted the service is up,
 * and if it is not quite, the worker's readiness probes notice the moment it is.
 * Only someone actually using the app wakes it, where a scheduled pinger would
 * keep it running around the clock and spend the free allowance doing so.
 *
 * Fire and forget. `no-cors` because the response is never read and the service
 * need not allow this origin. The promise it returns always resolves, failed
 * wake or not: a wake that does not land is a slower analysis, not a failed
 * one, and must never surface on the page as an error. Callers are free to
 * ignore it; it is returned so that promise can be checked.
 */
export function wakeAnalysisService(
  baseUrl: string | undefined = env.NEXT_PUBLIC_AI_SERVICE_URL,
  now: () => number = Date.now,
): Promise<void> {
  if (!baseUrl) return Promise.resolve();

  const at = now();
  if (at - lastWake < WAKE_TTL_MS) return Promise.resolve();
  lastWake = at;

  return fetch(`${baseUrl.replace(/\/+$/, "")}/health`, {
    mode: "no-cors",
    cache: "no-store",
  }).then(
    () => undefined,
    () => undefined,
  );
}

/** Forgets the last wake, so each test starts from a service nobody has woken. */
export function resetWake(): void {
  lastWake = Number.NEGATIVE_INFINITY;
}
