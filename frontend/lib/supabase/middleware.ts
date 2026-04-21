import { createServerClient, type CookieOptions } from "@supabase/ssr";
import { NextResponse, type NextRequest } from "next/server";

import { env } from "@/lib/env";
import { requestOrigin } from "@/lib/http/request-origin";

type CookiesToSet = { name: string; value: string; options: CookieOptions }[];

const PROTECTED_PREFIX = "/dashboard";

/**
 * Refreshes the Supabase session on every request and gates protected routes.
 * Returning the mutated response is what propagates rotated auth cookies back to
 * the browser, so callers must return this object verbatim.
 */
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
    },
  );

  const {
    data: { user },
  } = await supabase.auth.getUser();

  if (!user && request.nextUrl.pathname.startsWith(PROTECTED_PREFIX)) {
    // Build the redirect from the browser-facing origin, not request.nextUrl,
    // whose host is the container's internal bind address under Docker.
    const redirectUrl = new URL("/", requestOrigin(request));
    redirectUrl.searchParams.set("auth", "required");
    return NextResponse.redirect(redirectUrl);
  }

  return response;
}
