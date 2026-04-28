"use client";

import { Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { useEffect, useState } from "react";

import { useI18n } from "@/lib/i18n/context";
import { cn } from "@/lib/utils";

export function ThemeToggle({ className }: { className?: string }) {
  const { resolvedTheme, setTheme } = useTheme();
  const { t } = useI18n();
  const [mounted, setMounted] = useState(false);

  // Theme is unknown until after hydration; render an inert placeholder first
  // so the markup matches what the server sent.
  useEffect(() => setMounted(true), []);

  const isDark = resolvedTheme === "dark";

  return (
    <button
      type="button"
      aria-label={t.a11y.toggleTheme}
      onClick={() => setTheme(isDark ? "light" : "dark")}
      className={cn(
        "grid h-9 w-9 place-items-center rounded-full text-ink-muted transition-colors hover:bg-surface hover:text-ink",
        className,
      )}
    >
      {mounted ? (
        isDark ? (
          <Sun className="h-[18px] w-[18px]" aria-hidden />
        ) : (
          <Moon className="h-[18px] w-[18px]" aria-hidden />
        )
      ) : (
        <span className="h-[18px] w-[18px]" />
      )}
    </button>
  );
}
