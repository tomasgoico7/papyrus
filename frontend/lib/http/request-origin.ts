// request.url holds the internal bind address behind Docker/proxies, so prefer
// forwarded headers to build redirects the browser can actually reach.
export function requestOrigin(request: { headers: Headers; url: string }): string {
  const forwardedHost = request.headers.get("x-forwarded-host");
  const host = forwardedHost ?? request.headers.get("host");
  const proto = request.headers.get("x-forwarded-proto") ?? "http";
  return host ? `${proto}://${host}` : new URL(request.url).origin;
}
