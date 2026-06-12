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
    // Desktop pins the shell to the viewport; the panels scroll internally.
    <div className="min-h-screen lg:flex lg:h-screen lg:flex-col lg:overflow-hidden">
      <DashboardHeader user={profile} />
      <main className="lg:min-h-0 lg:flex-1">
        <Workspace userId={user.id} />
      </main>
    </div>
  );
}
