"use client";

import { Check, Copy, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { disableSharing, enableSharing } from "@/lib/analyses/repository";
import { useI18n } from "@/lib/i18n/context";
import { createClient } from "@/lib/supabase/client";
import { cn, formatDate } from "@/lib/utils";

const EXPIRY_OPTIONS = [7, 30, 90] as const;
const DEFAULT_EXPIRY = 30;

interface ShareDialogProps {
  open: boolean;
  analysisId: string;
  token: string | null;
  expiresAt: string | null;
  onClose: () => void;
  onChange: (token: string | null, expiresAt: string | null) => void;
}

export function ShareDialog({
  open,
  analysisId,
  token,
  expiresAt,
  onClose,
  onChange,
}: ShareDialogProps) {
  const { t } = useI18n();
  const supabase = useMemo(() => createClient(), []);
  const closeRef = useRef<HTMLButtonElement>(null);

  const [days, setDays] = useState<number>(DEFAULT_EXPIRY);
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  const isLive =
    token !== null &&
    expiresAt !== null &&
    Date.parse(expiresAt) > Date.now();

  const shareUrl =
    token && typeof window !== "undefined"
      ? `${window.location.origin}/share/${token}`
      : "";

  useEffect(() => {
    if (!open) return;

    closeRef.current?.focus();
    setCopied(false);

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKeyDown);

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.body.style.overflow = previousOverflow;
    };
  }, [open, onClose]);

  if (!open) return null;

  async function handleCreate() {
    setBusy(true);
    try {
      const info = await enableSharing(supabase, analysisId, days);
      onChange(info.token, info.expiresAt);
    } catch (error) {
      console.error(error);
    } finally {
      setBusy(false);
    }
  }

  async function handleStop() {
    setBusy(true);
    try {
      await disableSharing(supabase, analysisId);
      onChange(null, null);
    } catch (error) {
      console.error(error);
    } finally {
      setBusy(false);
    }
  }

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(shareUrl);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch (error) {
      console.error(error);
    }
  }

  return (
    <div className="fixed inset-0 z-50 grid place-items-center p-4">
      <div
        className="absolute inset-0 animate-fade-in bg-ink/40 backdrop-blur-sm"
        onClick={onClose}
        aria-hidden
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="share-dialog-title"
        className="relative w-full max-w-md animate-fade-up rounded-2xl border border-line bg-surface p-6 shadow-lifted"
      >
        <div className="flex items-start justify-between gap-4">
          <h2 id="share-dialog-title" className="text-lg font-medium">
            {t.share.dialogTitle}
          </h2>
          <button
            ref={closeRef}
            type="button"
            onClick={onClose}
            aria-label={t.share.close}
            className="-mr-1 grid h-8 w-8 shrink-0 place-items-center rounded-lg text-ink-faint transition-colors hover:bg-canvas hover:text-ink"
          >
            <X className="h-4 w-4" aria-hidden />
          </button>
        </div>
        <p className="mt-1 text-sm leading-relaxed text-ink-muted">
          {t.share.dialogDescription}
        </p>

        {isLive ? (
          <div className="mt-6 space-y-4">
            <div>
              <label className="mb-2 block text-sm font-medium">
                {t.share.linkLabel}
              </label>
              <div className="flex gap-2">
                <input
                  readOnly
                  value={shareUrl}
                  onFocus={(event) => event.target.select()}
                  className="min-w-0 flex-1 rounded-xl border border-line bg-canvas px-3 py-2.5 text-sm text-ink-muted outline-none"
                />
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={handleCopy}
                  className="shrink-0"
                >
                  {copied ? (
                    <>
                      <Check className="h-4 w-4" aria-hidden />
                      {t.share.copied}
                    </>
                  ) : (
                    <>
                      <Copy className="h-4 w-4" aria-hidden />
                      {t.share.copy}
                    </>
                  )}
                </Button>
              </div>
            </div>

            <div className="flex items-center justify-between gap-4 border-t border-line pt-4">
              <span className="text-xs text-ink-faint">
                {expiresAt
                  ? t.share.expiresOn.replace("{date}", formatDate(expiresAt))
                  : null}
              </span>
              <button
                type="button"
                onClick={handleStop}
                disabled={busy}
                className="inline-flex items-center gap-1.5 text-sm font-medium text-danger transition-opacity hover:opacity-70 disabled:opacity-50"
              >
                {busy ? <Spinner className="h-3.5 w-3.5" /> : null}
                {t.share.stop}
              </button>
            </div>
          </div>
        ) : (
          <div className="mt-6 space-y-5">
            <div>
              <label className="mb-2 block text-sm font-medium">
                {t.share.expiryLabel}
              </label>
              <div className="grid grid-cols-3 gap-2">
                {EXPIRY_OPTIONS.map((option) => (
                  <button
                    key={option}
                    type="button"
                    onClick={() => setDays(option)}
                    aria-pressed={days === option}
                    className={cn(
                      "rounded-xl border px-3 py-2.5 text-sm font-medium transition-colors",
                      days === option
                        ? "border-accent bg-accent-soft text-accent"
                        : "border-line text-ink-muted hover:border-ink-faint",
                    )}
                  >
                    {t.share.daysOption.replace("{days}", String(option))}
                  </button>
                ))}
              </div>
            </div>

            <Button
              type="button"
              onClick={handleCreate}
              disabled={busy}
              className="w-full"
            >
              {busy ? (
                <>
                  <Spinner />
                  {t.share.creating}
                </>
              ) : (
                t.share.create
              )}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
