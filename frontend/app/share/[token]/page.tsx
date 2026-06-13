import type { Metadata } from "next";

import { SharedAnalysis } from "@/components/share/shared-analysis";
import { getSharedAnalysis } from "@/lib/analyses/repository";
import { createClient } from "@/lib/supabase/server";

export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "Papyrus",
  robots: { index: false },
};

export default async function SharePage({
  params,
}: {
  params: { token: string };
}) {
  const supabase = createClient();
  const data = await getSharedAnalysis(supabase, params.token);

  return <SharedAnalysis data={data} />;
}
