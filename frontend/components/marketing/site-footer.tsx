import { Logo } from "@/components/ui/logo";
import { getServerDictionary } from "@/lib/i18n/server";

export function SiteFooter() {
  const t = getServerDictionary();

  return (
    <footer className="border-t border-line/70">
      <div className="mx-auto flex max-w-content flex-col items-center justify-between gap-4 px-6 py-10 text-sm text-ink-faint sm:flex-row">
        <Logo className="text-sm" />
        <p>{t.landing.footerTagline}</p>
      </div>
    </footer>
  );
}
