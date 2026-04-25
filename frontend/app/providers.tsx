"use client";

import { ThemeProvider } from "next-themes";

import type { Locale } from "@/lib/i18n/config";
import { I18nProvider } from "@/lib/i18n/context";

export function Providers({
  initialLocale,
  children,
}: {
  initialLocale: Locale;
  children: React.ReactNode;
}) {
  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="system"
      enableSystem
      disableTransitionOnChange
    >
      <I18nProvider initialLocale={initialLocale}>{children}</I18nProvider>
    </ThemeProvider>
  );
}
