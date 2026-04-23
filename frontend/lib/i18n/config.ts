export const LOCALES = ["en", "es"] as const;

export type Locale = (typeof LOCALES)[number];

export const DEFAULT_LOCALE: Locale = "en";

export const LOCALE_COOKIE = "papyrus.locale";

export function isLocale(value: string | undefined | null): value is Locale {
  return value === "en" || value === "es";
}
