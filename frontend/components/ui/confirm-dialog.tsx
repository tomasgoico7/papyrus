"use client";

import { useEffect, useRef } from "react";

import { Button } from "@/components/ui/button";

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description: string;
  confirmLabel: string;
  cancelLabel: string;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  cancelLabel,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  // Default focus lands on Cancel — the safe choice for a destructive action.
  const cancelRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;

    cancelRef.current?.focus();

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onCancel();
    };
    document.addEventListener("keydown", onKeyDown);

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.body.style.overflow = previousOverflow;
    };
  }, [open, onCancel]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 grid place-items-center p-4">
      <div
        className="absolute inset-0 animate-fade-in bg-ink/40 backdrop-blur-sm"
        onClick={onCancel}
        aria-hidden
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-dialog-title"
        className="relative w-full max-w-sm animate-fade-up rounded-2xl border border-line bg-surface p-6 shadow-lifted"
      >
        <h2 id="confirm-dialog-title" className="text-lg font-medium">
          {title}
        </h2>
        <p className="mt-2 text-sm leading-relaxed text-ink-muted">
          {description}
        </p>
        <div className="mt-6 flex justify-end gap-3">
          <Button
            ref={cancelRef}
            variant="secondary"
            size="sm"
            onClick={onCancel}
          >
            {cancelLabel}
          </Button>
          <Button variant="destructive" size="sm" onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}
