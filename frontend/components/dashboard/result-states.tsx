"use client";

import { Sparkles } from "lucide-react";

import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/lib/i18n/context";

export function EmptyState() {
  const { t } = useI18n();
  return (
    <div className="grid min-h-[28rem] animate-fade-in place-items-center rounded-3xl border border-line bg-surface/60 px-6 text-center lg:h-full">
      <div className="max-w-xs">
        <span className="mx-auto grid h-14 w-14 place-items-center rounded-2xl bg-accent-soft text-accent">
          <Sparkles className="h-6 w-6" aria-hidden />
        </span>
        <h2 className="mt-6 text-lg font-medium">{t.result.emptyTitle}</h2>
        <p className="mt-2 text-sm leading-relaxed text-ink-muted">
          {t.result.emptyBody}
        </p>
      </div>
    </div>
  );
}

export function AnalyzingState() {
  const { t } = useI18n();
  return (
    <div className="grid min-h-[28rem] animate-fade-in place-items-center rounded-3xl border border-line bg-surface px-6 text-center shadow-subtle lg:h-full">
      <div className="flex max-w-xs flex-col items-center">
        <Spinner className="h-6 w-6 text-accent" />
        <h2 className="mt-6 text-lg font-medium">{t.result.loadingTitle}</h2>
        <p className="mt-2 text-sm leading-relaxed text-ink-muted">
          {t.result.loadingBody}
        </p>
      </div>
    </div>
  );
}

export function CvNotice({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-3 rounded-2xl border border-line bg-surface px-4 py-3">
      <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-warning" aria-hidden />
      <p className="text-sm text-ink-muted">{message}</p>
    </div>
  );
}

export function ErrorState({ message }: { message: string }) {
  const { t } = useI18n();
  return (
    <div className="grid min-h-[28rem] animate-fade-in place-items-center rounded-3xl border border-warning/30 bg-warning/5 px-6 text-center lg:h-full">
      <div className="max-w-xs">
        <h2 className="text-lg font-medium text-ink">{t.result.errorTitle}</h2>
        <p className="mt-2 text-sm leading-relaxed text-ink-muted">{message}</p>
      </div>
    </div>
  );
}
