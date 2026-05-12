import { redirect } from "next/navigation";

import { DashboardHeader } from "@/components/dashboard/dashboard-header";
import { Workspace } from "@/components/dashboard/workspace";
import { createClient } from "@/lib/supabase/server";
import type { AuthenticatedUser } from "@/lib/types";

export const dynamic = "force-dynamic";

export default async function DashboardPage() {
  const supabase = createClient();
  const {
    data: { user },
  } = await supabase.auth.getUser();

  // The middleware already guards this route; this is defence in depth and lets
  // us hand a fully-typed user down to the tree.
  if (!user) {
    redirect("/");
  }

  const profile: AuthenticatedUser = {
    id: user.id,
    email: user.email ?? "",
    fullName: (user.user_metadata.full_name as string | undefined) ?? null,
    avatarUrl: (user.user_metadata.avatar_url as string | undefined) ?? null,
  };

  return (
    <div className="min-h-screen">
      <DashboardHeader user={profile} />
      <main>
        <Workspace userId={user.id} />
      </main>
    </div>
  );
}
