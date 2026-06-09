"use client";

import { LogOut } from "lucide-react";

import { useI18n } from "@/lib/i18n/context";
import { cn } from "@/lib/utils";

export function SignOutButton({ className }: { className?: string }) {
  const { t } = useI18n();

  return (
    <form action="/auth/signout" method="post">
      <button
        type="submit"
        aria-label={t.nav.signOut}
        title={t.nav.signOut}
        className={cn(
          "grid h-9 w-9 place-items-center rounded-full text-ink-muted transition-colors hover:bg-surface hover:text-ink",
          className,
        )}
      >
        <LogOut className="h-[18px] w-[18px]" aria-hidden />
      </button>
    </form>
  );
}
