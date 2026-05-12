"use client";

import { FileText, UploadCloud, X } from "lucide-react";
import { useRef, useState } from "react";

import { useI18n } from "@/lib/i18n/context";
import { cn } from "@/lib/utils";

interface CvDropzoneProps {
  file: File | null;
  onSelect: (file: File | null) => void;
  maxSizeMb: number;
  disabled?: boolean;
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

export function CvDropzone({
  file,
  onSelect,
  maxSizeMb,
  disabled,
}: CvDropzoneProps) {
  const { t } = useI18n();
  const inputRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function validateAndSelect(candidate: File | undefined) {
    if (!candidate) return;
    if (candidate.type !== "application/pdf") {
      setError(t.dashboard.errPdfOnly);
      return;
    }
    if (candidate.size > maxSizeMb * 1024 * 1024) {
      setError(t.dashboard.errTooLarge.replace("{mb}", String(maxSizeMb)));
      return;
    }
    setError(null);
    onSelect(candidate);
  }

  if (file) {
    return (
      <div className="flex items-center gap-3 rounded-2xl border border-line bg-surface px-4 py-3.5">
        <FileText className="h-5 w-5 shrink-0 text-accent" aria-hidden />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium">{file.name}</p>
          <p className="text-xs text-ink-faint">{formatSize(file.size)}</p>
        </div>
        <button
          type="button"
          onClick={() => onSelect(null)}
          disabled={disabled}
          className="rounded-full p-1.5 text-ink-faint transition-colors hover:bg-canvas hover:text-ink"
          aria-label={t.dashboard.removeFile}
        >
          <X className="h-4 w-4" />
        </button>
      </div>
    );
  }

  return (
    <div>
      <button
        type="button"
        disabled={disabled}
        onClick={() => inputRef.current?.click()}
        onDragOver={(event) => {
          event.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(event) => {
          event.preventDefault();
          setDragging(false);
          validateAndSelect(event.dataTransfer.files[0]);
        }}
        className={cn(
          "flex w-full flex-col items-center gap-2 rounded-2xl border border-dashed px-6 py-10 text-center transition-colors",
          dragging
            ? "border-accent bg-accent-soft"
            : "border-line hover:border-ink-faint",
        )}
      >
        <UploadCloud className="h-6 w-6 text-ink-faint" aria-hidden />
        <span className="text-sm font-medium">{t.dashboard.dropTitle}</span>
        <span className="text-xs text-ink-faint">
          {t.dashboard.dropHint.replace("{mb}", String(maxSizeMb))}
        </span>
      </button>
      <input
        ref={inputRef}
        type="file"
        accept="application/pdf"
        className="hidden"
        onChange={(event) => validateAndSelect(event.target.files?.[0])}
      />
      {error ? <p className="mt-2 text-sm text-warning">{error}</p> : null}
    </div>
  );
}
