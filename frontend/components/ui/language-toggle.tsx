"use client";

import { LOCALES } from "@/lib/i18n/config";
import { useI18n } from "@/lib/i18n/context";
import { cn } from "@/lib/utils";

export function LanguageToggle({ className }: { className?: string }) {
  const { locale, setLocale, t } = useI18n();

  return (
    <div
      role="group"
      aria-label={t.a11y.switchLanguage}
      className={cn(
        "flex items-center rounded-full border border-line p-0.5 text-xs font-medium",
        className,
      )}
    >
      {LOCALES.map((code) => (
        <button
          key={code}
          type="button"
          onClick={() => setLocale(code)}
          aria-pressed={locale === code}
          className={cn(
            "rounded-full px-2 py-1 uppercase tracking-wide transition-colors",
            locale === code
              ? "bg-ink text-canvas"
              : "text-ink-faint hover:text-ink",
          )}
        >
          {code}
        </button>
      ))}
    </div>
  );
}
