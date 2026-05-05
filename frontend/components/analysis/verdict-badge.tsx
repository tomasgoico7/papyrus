"use client";

import { useI18n } from "@/lib/i18n/context";
import type { Verdict } from "@/lib/types";
import { cn } from "@/lib/utils";

const VERDICT_CLASS: Record<Verdict, string> = {
  strong: "border-accent/20 bg-accent-soft text-accent",
  moderate: "border-line bg-surface text-ink-muted",
  weak: "border-warning/25 bg-warning/10 text-warning",
};

export function VerdictBadge({ verdict }: { verdict: Verdict }) {
  const { t } = useI18n();
  return (
    <span className={cn("chip font-medium", VERDICT_CLASS[verdict])}>
      {t.analysis.verdict[verdict]}
    </span>
  );
}
