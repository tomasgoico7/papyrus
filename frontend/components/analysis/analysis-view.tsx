"use client";

import { Download } from "lucide-react";

import { ScoreRing } from "@/components/analysis/score-ring";
import { SkillList } from "@/components/analysis/skill-list";
import { SuggestionCard } from "@/components/analysis/suggestion-card";
import { VerdictBadge } from "@/components/analysis/verdict-badge";
import { useI18n } from "@/lib/i18n/context";
import type { Localized, LocalizedList, Suggestion, Verdict } from "@/lib/types";
import { formatDate } from "@/lib/utils";

export interface AnalysisViewData {
  jobTitle?: string | null;
  cvFilename?: string | null;
  score: number;
  verdict: Verdict;
  summary: Localized;
  matchedSkills: LocalizedList;
  missingSkills: LocalizedList;
  suggestions: Suggestion[];
  createdAt?: string;
}

interface AnalysisViewProps {
  data: AnalysisViewData;
  onDownloadCv?: () => void;
}

export function AnalysisView({ data, onDownloadCv }: AnalysisViewProps) {
  const { t, locale } = useI18n();

  return (
    <article className="animate-fade-up space-y-10">
      <header className="flex flex-col gap-6 border-b border-line pb-8 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-2">
          <VerdictBadge verdict={data.verdict} />
          <h2 className="text-2xl font-semibold tracking-tight">
            {data.jobTitle?.trim() || t.analysis.defaultTitle}
          </h2>
          <p className="text-sm text-ink-faint">
            {data.cvFilename}
            {data.createdAt ? ` · ${formatDate(data.createdAt)}` : null}
          </p>
          {onDownloadCv ? (
            <button
              type="button"
              onClick={onDownloadCv}
              className="inline-flex items-center gap-1.5 text-sm font-medium text-accent transition-opacity hover:opacity-70"
            >
              <Download className="h-3.5 w-3.5" aria-hidden />
              {t.analysis.downloadCv}
            </button>
          ) : null}
        </div>
        <ScoreRing score={data.score} size={128} caption={t.analysis.fit} />
      </header>

      <section>
        <h3 className="text-xs font-medium uppercase tracking-[0.12em] text-ink-faint">
          {t.analysis.summary}
        </h3>
        <p className="mt-3 text-lg leading-relaxed text-ink">
          {data.summary[locale]}
        </p>
      </section>

      <section className="grid gap-8 sm:grid-cols-2">
        <SkillList
          title={t.analysis.matched}
          tone="positive"
          skills={data.matchedSkills[locale]}
          emptyHint={t.analysis.matchedEmpty}
        />
        <SkillList
          title={t.analysis.missing}
          tone="warning"
          skills={data.missingSkills[locale]}
          emptyHint={t.analysis.missingEmpty}
        />
      </section>

      {data.suggestions.length > 0 ? (
        <section>
          <h3 className="text-xs font-medium uppercase tracking-[0.12em] text-ink-faint">
            {t.analysis.improve}
          </h3>
          <ul className="mt-4 space-y-3">
            {data.suggestions.map((suggestion, index) => (
              <SuggestionCard key={index} suggestion={suggestion} />
            ))}
          </ul>
        </section>
      ) : null}
    </article>
  );
}
