import Link from "next/link";

import { SignInButton } from "@/components/auth/sign-in-button";
import { HeroPreview } from "@/components/marketing/hero-preview";
import { Reveal } from "@/components/marketing/reveal";
import { SiteFooter } from "@/components/marketing/site-footer";
import { SiteHeader } from "@/components/marketing/site-header";
import { buttonClasses } from "@/components/ui/button";
import { getServerDictionary } from "@/lib/i18n/server";
import { createClient } from "@/lib/supabase/server";
import type { AuthenticatedUser } from "@/lib/types";

export default async function LandingPage() {
  const t = getServerDictionary();
  const supabase = createClient();
  const {
    data: { user },
  } = await supabase.auth.getUser();

  const profile: AuthenticatedUser | null = user
    ? {
        id: user.id,
        email: user.email ?? "",
        fullName: (user.user_metadata.full_name as string | undefined) ?? null,
        avatarUrl: (user.user_metadata.avatar_url as string | undefined) ?? null,
      }
    : null;

  const primaryCta = user ? (
    <Link href="/dashboard" className={buttonClasses("primary", "lg")}>
      {t.nav.openDashboard}
    </Link>
  ) : (
    <SignInButton label={t.landing.ctaPrimary} size="lg" />
  );

  return (
    <div className="flex min-h-screen flex-col">
      <SiteHeader user={profile} />

      <main className="flex-1">
        <section className="mx-auto grid max-w-content items-center gap-x-16 gap-y-14 px-6 pb-20 pt-16 lg:grid-cols-[1.05fr_0.95fr] lg:pb-24 lg:pt-24">
          <div>
            <p className="animate-fade-up text-sm font-medium uppercase tracking-[0.16em] text-ink-faint">
              {t.landing.eyebrow}
            </p>
            <h1 className="mt-5 animate-fade-up text-balance text-5xl font-semibold leading-[1.04] tracking-tightest [animation-delay:60ms] sm:text-6xl lg:text-[4.25rem]">
              {t.landing.title}
            </h1>
            <p className="mt-6 max-w-xl animate-fade-up text-lg leading-relaxed text-ink-muted [animation-delay:120ms]">
              {t.landing.subtitle}
            </p>
            <div className="mt-9 flex animate-fade-up flex-wrap items-center gap-3 [animation-delay:180ms]">
              {primaryCta}
              <Link
                href="#how-it-works"
                className="text-[0.95rem] font-medium text-ink-muted transition-colors hover:text-ink"
              >
                {t.landing.seeHow}
              </Link>
            </div>
            <p className="mt-6 animate-fade-up text-sm text-ink-faint [animation-delay:240ms]">
              {t.landing.freeNote}
            </p>
          </div>

          <div className="animate-fade-up [animation-delay:240ms]">
            <HeroPreview t={t} />
          </div>
        </section>

        <section
          id="how-it-works"
          className="scroll-mt-20 border-t border-line/60 bg-surface/40"
        >
          <div className="mx-auto max-w-content px-6 py-20 lg:py-24">
            <Reveal>
              <h2 className="max-w-2xl text-3xl font-semibold tracking-tight sm:text-4xl">
                {t.landing.stepsTitle}
              </h2>
            </Reveal>
            <div className="mt-14 grid gap-12 sm:grid-cols-3">
              {t.landing.steps.map((step, index) => (
                <Reveal key={step.title} delay={index * 120}>
                  <span className="text-sm font-medium tabular-nums text-accent">
                    {String(index + 1).padStart(2, "0")}
                  </span>
                  <h3 className="mt-3 text-lg font-medium">{step.title}</h3>
                  <p className="mt-2 leading-relaxed text-ink-muted">
                    {step.body}
                  </p>
                </Reveal>
              ))}
            </div>
          </div>
        </section>
      </main>

      <SiteFooter />
    </div>
  );
}
