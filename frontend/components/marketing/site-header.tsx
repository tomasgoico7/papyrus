import Link from "next/link";

import { LanguageToggle } from "@/components/ui/language-toggle";
import { Logo } from "@/components/ui/logo";
import { ThemeToggle } from "@/components/ui/theme-toggle";

export function SiteHeader() {
  return (
    <header className="sticky top-0 z-50 border-b border-line/60 bg-canvas/70 backdrop-blur-xl">
      <div className="mx-auto flex h-16 max-w-content items-center justify-between px-6">
        <Link href="/" aria-label="Papyrus">
          <Logo />
        </Link>
        <div className="flex items-center gap-2">
          <LanguageToggle />
          <ThemeToggle />
        </div>
      </div>
    </header>
  );
}
