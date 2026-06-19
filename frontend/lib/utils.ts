import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

import type { Locale } from "@/lib/i18n/config";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

const MONTHS: Record<Locale, readonly string[]> = {
  en: ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"],
  es: ["ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"],
};

// Formatted from explicit month names in UTC so the server and the client always
// produce the same string — toLocaleDateString picks a different default locale
// and timezone on each, which breaks hydration on server-rendered pages.
export function formatDate(iso: string, locale: Locale = "en"): string {
  const date = new Date(iso);
  const day = date.getUTCDate();
  const month = MONTHS[locale][date.getUTCMonth()];
  const year = date.getUTCFullYear();
  return locale === "en" ? `${month} ${day}, ${year}` : `${day} ${month} ${year}`;
}

/**
 * Splits a "Label: rest" line into its bold label and the remainder, or null
 * when there is no colon. Used to render grouped skill lines on the CV.
 */
export function splitLabel(
  value: string,
): { label: string; rest: string } | null {
  const index = value.indexOf(":");
  if (index === -1) return null;
  return { label: value.slice(0, index + 1), rest: value.slice(index + 1) };
}

/** Builds a professional CV filename from a name, e.g. "Tomas_Goicoechea_CV". */
export function cvFileName(fullName: string): string {
  const cleaned = fullName
    .normalize("NFD")
    .replace(/\p{Diacritic}/gu, "")
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim();
  return cleaned ? `${cleaned.split(/\s+/).join("_")}_CV` : "CV";
}

/** Turns arbitrary text into a filename-safe, accent-free slug. */
export function slugify(value: string): string {
  return (
    value
      .toLowerCase()
      .normalize("NFD")
      .replace(/\p{Diacritic}/gu, "")
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 50) || "analysis"
  );
}
