"use client";

import { useI18n } from "@/lib/i18n/context";
import type { Priority, Suggestion } from "@/lib/types";
import { cn } from "@/lib/utils";

const PRIORITY_CLASS: Record<Priority, string> = {
  high: "bg-warning/10 text-warning",
  medium: "bg-accent-soft text-accent",
  low: "bg-canvas text-ink-faint",
};

export function SuggestionCard({ suggestion }: { suggestion: Suggestion }) {
  const { t, locale } = useI18n();

  return (
    <li className="rounded-2xl border border-line bg-surface p-5 transition-shadow duration-300 hover:shadow-subtle">
      <div className="flex items-start justify-between gap-4">
        <h4 className="min-w-0 font-medium leading-snug">{suggestion.title[locale]}</h4>
        <span
          className={cn(
            "shrink-0 rounded-full px-2.5 py-1 text-xs font-medium",
            PRIORITY_CLASS[suggestion.priority],
          )}
        >
          {t.analysis.priority[suggestion.priority]}
        </span>
      </div>
      <p className="mt-2 text-sm leading-relaxed text-ink-muted">
        {suggestion.detail[locale]}
      </p>
    </li>
  );
}
