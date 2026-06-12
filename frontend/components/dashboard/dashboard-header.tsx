import Link from "next/link";

import { SignOutButton } from "@/components/auth/sign-out-button";
import { Avatar } from "@/components/ui/avatar";
import { LanguageToggle } from "@/components/ui/language-toggle";
import { Logo } from "@/components/ui/logo";
import { ThemeToggle } from "@/components/ui/theme-toggle";
import type { AuthenticatedUser } from "@/lib/types";

export function DashboardHeader({ user }: { user: AuthenticatedUser }) {
  return (
    <header className="sticky top-0 z-40 border-b border-line/70 bg-canvas/80 backdrop-blur-xl">
      <div className="mx-auto flex h-16 w-full max-w-[110rem] items-center justify-between px-6 lg:px-10">
        <Link href="/" aria-label="Papyrus">
          <Logo />
        </Link>

        <div className="flex items-center gap-2">
          <LanguageToggle />
          <ThemeToggle />
          <div className="ml-1 flex items-center gap-2 border-l border-line pl-3">
            <Avatar user={user} />
            <SignOutButton />
          </div>
        </div>
      </div>
    </header>
  );
}
