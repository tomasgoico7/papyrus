import { NextResponse } from "next/server";

import { requestOrigin } from "@/lib/http/request-origin";
import { createClient } from "@/lib/supabase/server";

export async function POST(request: Request) {
  const supabase = createClient();
  await supabase.auth.signOut();

  // 303 forces the browser to follow with a GET to the landing page.
  return NextResponse.redirect(`${requestOrigin(request)}/`, { status: 303 });
}
