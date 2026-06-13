"use client";

import Link from "next/link";

import { AnalysisView, type AnalysisViewData } from "@/components/analysis/analysis-view";
import { buttonClasses } from "@/components/ui/button";
import { LanguageToggle } from "@/components/ui/language-toggle";
import { Logo } from "@/components/ui/logo";
import { ThemeToggle } from "@/components/ui/theme-toggle";
import { useI18n } from "@/lib/i18n/context";

export function SharedAnalysis({ data }: { data: AnalysisViewData | null }) {
  const { t } = useI18n();

  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-40 border-b border-line/70 bg-canvas/80 backdrop-blur-xl">
        <div className="mx-auto flex h-16 max-w-3xl items-center justify-between px-6">
          <Link href="/" aria-label="Papyrus">
            <Logo />
          </Link>
          <div className="flex items-center gap-2">
            <LanguageToggle />
            <ThemeToggle />
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-3xl px-6 py-10 lg:py-14">
        {data ? (
          <>
            <AnalysisView data={data} />

            <div className="mt-12 flex flex-col items-center gap-4 rounded-2xl border border-line bg-surface p-6 text-center sm:flex-row sm:justify-between sm:text-left">
              <p className="text-sm font-medium text-ink">{t.share.ctaTitle}</p>
              <Link
                href="/"
                className={buttonClasses("primary", "md") + " shrink-0"}
              >
                {t.share.ctaButton}
              </Link>
            </div>

            <p className="mt-8 text-center text-xs text-ink-faint">
              {t.share.poweredBy}
            </p>
          </>
        ) : (
          <div className="grid min-h-[55vh] place-items-center text-center">
            <div className="max-w-sm">
              <h1 className="text-xl font-semibold">
                {t.share.unavailableTitle}
              </h1>
              <p className="mt-2 text-sm leading-relaxed text-ink-muted">
                {t.share.unavailableBody}
              </p>
              <Link
                href="/"
                className={buttonClasses("primary", "md") + " mt-6"}
              >
                {t.share.ctaButton}
              </Link>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
