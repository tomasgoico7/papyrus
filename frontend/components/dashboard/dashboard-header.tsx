"use client";

import Link from "next/link";

import { LanguageToggle } from "@/components/ui/language-toggle";
import { Logo } from "@/components/ui/logo";
import { ThemeToggle } from "@/components/ui/theme-toggle";
import { useI18n } from "@/lib/i18n/context";
import type { AuthenticatedUser } from "@/lib/types";

export function DashboardHeader({ user }: { user: AuthenticatedUser }) {
  const { t } = useI18n();

  return (
    <header className="sticky top-0 z-40 border-b border-line/70 bg-canvas/80 backdrop-blur-xl">
      <div className="mx-auto flex h-16 max-w-content items-center justify-between px-6">
        <Link href="/" aria-label="Papyrus">
          <Logo />
        </Link>

        <div className="flex items-center gap-3">
          <LanguageToggle />
          <ThemeToggle />
          <div className="hidden items-center gap-2.5 sm:flex">
            <Avatar user={user} />
            <span className="text-sm text-ink-muted">
              {user.fullName ?? user.email}
            </span>
          </div>
          <form action="/auth/signout" method="post">
            <button
              type="submit"
              className="text-sm font-medium text-ink-faint transition-colors hover:text-ink"
            >
              {t.nav.signOut}
            </button>
          </form>
        </div>
      </div>
    </header>
  );
}

function Avatar({ user }: { user: AuthenticatedUser }) {
  if (user.avatarUrl) {
    return (
      <img
        src={user.avatarUrl}
        alt=""
        className="h-8 w-8 rounded-full object-cover"
      />
    );
  }

  const initial = (user.fullName ?? user.email).charAt(0).toUpperCase();
  return (
    <span className="grid h-8 w-8 place-items-center rounded-full bg-ink text-sm font-medium text-canvas">
      {initial}
    </span>
  );
}
