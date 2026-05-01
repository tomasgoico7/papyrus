import { NextResponse } from "next/server";

import { requestOrigin } from "@/lib/http/request-origin";
import { createClient } from "@/lib/supabase/server";

/**
 * OAuth redirect target. Supabase sends the user here with a `code` that we
 * exchange for a session cookie before forwarding them on to the app.
 */
export async function GET(request: Request) {
  const { searchParams } = new URL(request.url);
  const code = searchParams.get("code");
  const next = searchParams.get("next") ?? "/dashboard";
  const origin = requestOrigin(request);

  if (code) {
    const supabase = createClient();
    const { error } = await supabase.auth.exchangeCodeForSession(code);
    if (!error) {
      return NextResponse.redirect(`${origin}${next}`);
    }
  }

  return NextResponse.redirect(`${origin}/?auth=error`);
}
