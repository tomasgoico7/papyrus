"use client";

import { useEffect, useRef } from "react";

import { ScoreRing } from "@/components/analysis/score-ring";
import type { Dictionary } from "@/lib/i18n/dictionaries";

const PARALLAX_FACTOR = 0.06;

export function HeroPreview({ t }: { t: Dictionary }) {
  const ref = useRef<HTMLDivElement>(null);

  // Gentle scroll parallax: the card drifts up as the page scrolls. scrollY is
  // cheap to read and rAF-throttled, so this stays smooth.
  useEffect(() => {
    const node = ref.current;
    if (!node) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;

    let frame = 0;
    const update = () => {
      frame = 0;
      node.style.transform = `translate3d(0, ${-window.scrollY * PARALLAX_FACTOR}px, 0)`;
    };
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };

    update();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      window.removeEventListener("scroll", onScroll);
      if (frame) cancelAnimationFrame(frame);
    };
  }, []);

  return (
    <div ref={ref} className="will-change-transform">
      <div className="animate-float motion-reduce:animate-none">
        <div className="mx-auto w-full max-w-md rounded-3xl border border-line bg-surface p-8 shadow-lifted">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-ink-faint">{t.landing.preview.role}</p>
              <p className="mt-1 font-medium">jane-doe-resume.pdf</p>
            </div>
            <span className="chip border-accent/20 bg-accent-soft text-accent">
              {t.landing.preview.verdict}
            </span>
          </div>

          <div className="mt-8 flex justify-center">
            <ScoreRing score={82} caption={t.analysis.fit} />
          </div>

          <div className="mt-8 space-y-4">
            <PreviewRow
              label={t.landing.preview.matched}
              tone="positive"
              items={["Go", "PostgreSQL", "Docker", "CI/CD"]}
            />
            <PreviewRow
              label={t.landing.preview.missing}
              tone="warning"
              items={["Kubernetes", "gRPC"]}
            />
          </div>
        </div>
      </div>
    </div>
  );
}

function PreviewRow({
  label,
  tone,
  items,
}: {
  label: string;
  tone: "positive" | "warning";
  items: string[];
}) {
  const dot = tone === "positive" ? "bg-positive" : "bg-warning";
  return (
    <div>
      <p className="mb-2 text-xs font-medium uppercase tracking-[0.12em] text-ink-faint">
        {label}
      </p>
      <div className="flex flex-wrap gap-2">
        {items.map((item) => (
          <span key={item} className="chip border-line text-ink-muted">
            <span className={`h-1.5 w-1.5 rounded-full ${dot}`} />
            {item}
          </span>
        ))}
      </div>
    </div>
  );
}
