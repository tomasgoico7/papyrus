"use client";

import { useEffect, useState } from "react";

import { cn } from "@/lib/utils";

interface ScoreRingProps {
  score: number;
  size?: number;
  caption?: string;
  className?: string;
}

const DURATION_MS = 1100;

export function ScoreRing({
  score,
  size = 168,
  caption,
  className,
}: ScoreRingProps) {
  const target = Math.max(0, Math.min(100, Math.round(score)));
  const stroke = size * 0.06;
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;

  // Eased progress (0 → 1) driving both the ring sweep and the count-up.
  const [progress, setProgress] = useState(0);

  useEffect(() => {
    const reduceMotion =
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (reduceMotion) {
      setProgress(1);
      return;
    }

    let frame = 0;
    const start = performance.now();
    const tick = (now: number) => {
      const t = Math.min(1, (now - start) / DURATION_MS);
      setProgress(1 - Math.pow(1 - t, 3)); // easeOutCubic
      if (t < 1) frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [target]);

  const current = target * progress;
  const offset = circumference * (1 - current / 100);

  return (
    <div
      className={cn("relative inline-grid place-items-center", className)}
      style={{ width: size, height: size }}
    >
      <svg width={size} height={size} className="-rotate-90">
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          strokeWidth={stroke}
          className="stroke-line"
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          className="stroke-accent"
        />
      </svg>
      <div className="absolute inset-0 grid place-items-center text-center">
        <div>
          <span className="text-[2.6rem] font-semibold leading-none tracking-tightest tabular-nums">
            {Math.round(current)}
          </span>
          <span className="ml-0.5 text-base font-medium text-ink-faint">
            /100
          </span>
          {caption ? (
            <p className="mt-1 text-xs uppercase tracking-[0.14em] text-ink-faint">
              {caption}
            </p>
          ) : null}
        </div>
      </div>
    </div>
  );
}
