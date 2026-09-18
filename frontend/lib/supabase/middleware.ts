import { createServerClient, type CookieOptions } from "@supabase/ssr";
import { NextResponse, type NextRequest } from "next/server";

import { env } from "@/lib/env";
import { requestOrigin } from "@/lib/http/request-origin";

type CookiesToSet = { name: string; value: string; options: CookieOptions }[];

const PROTECTED_PREFIX = "/dashboard";

// The auth check runs on every request, so it needs a ceiling. Without one, an
// unreachable Supabase project left the call hanging until the platform killed
// the middleware, and every page — the public landing included — answered 504.
const AUTH_TIMEOUT_MS = 2500;

export async function updateSession(request: NextRequest) {
  let response = NextResponse.next({ request });

  const supabase = createServerClient(
    env.NEXT_PUBLIC_SUPABASE_URL,
    env.NEXT_PUBLIC_SUPABASE_ANON_KEY,
    {
      cookies: {
        getAll() {
          return request.cookies.getAll();
        },
        setAll(cookiesToSet: CookiesToSet) {
          cookiesToSet.forEach(({ name, value }) =>
            request.cookies.set(name, value),
          );
          response = NextResponse.next({ request });
          cookiesToSet.forEach(({ name, value, options }) =>
            response.cookies.set(name, value, options),
          );
        },
      },
      global: {
        fetch: (input: RequestInfo | URL, init?: RequestInit) =>
          fetch(input, { ...init, signal: AbortSignal.timeout(AUTH_TIMEOUT_MS) }),
      },
    },
  );

  const isProtected = request.nextUrl.pathname.startsWith(PROTECTED_PREFIX);

  let user = null;
  try {
    const { data } = await supabase.auth.getUser();
    user = data.user;
  } catch {
    // Auth is unreachable rather than the visitor being signed out. Public pages
    // have no business failing over it; the dashboard still cannot be served,
    // and falls through to the redirect below.
    if (!isProtected) {
      return response;
    }
  }

  if (!user && isProtected) {
    const redirectUrl = new URL("/", requestOrigin(request));
    redirectUrl.searchParams.set("auth", "required");
    return NextResponse.redirect(redirectUrl);
  }

  return response;
}
