/**
 * Resolves the browser-facing origin from request headers. Behind Docker
 * (the Next standalone server binds to HOSTNAME=0.0.0.0) or a proxy, the host in
 * `request.url` is the internal bind address, so any redirect built from it would
 * send the browser to an unreachable `0.0.0.0`. The forwarded/Host header carries
 * the address the browser actually used.
 *
 * Accepts anything with `headers`/`url`, so it works for both the Web `Request`
 * in route handlers and `NextRequest` in middleware.
 */
export function requestOrigin(request: { headers: Headers; url: string }): string {
  const forwardedHost = request.headers.get("x-forwarded-host");
  const host = forwardedHost ?? request.headers.get("host");
  const proto = request.headers.get("x-forwarded-proto") ?? "http";
  return host ? `${proto}://${host}` : new URL(request.url).origin;
}
