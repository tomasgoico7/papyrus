"use client";

import { ArrowRight, FileText, X } from "lucide-react";

import { CvDropzone } from "@/components/dashboard/cv-dropzone";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import type { StoredCvSummary } from "@/lib/cvs/repository";
import { MIN_OFFER_LENGTH } from "@/lib/constants";
import { useI18n } from "@/lib/i18n/context";
import { formatDate } from "@/lib/utils";

interface AnalyzerFormProps {
  cvFile: File | null;
  onCvChange: (file: File | null) => void;
  storedCvs: StoredCvSummary[];
  selectedCv: StoredCvSummary | null;
  onSelectStoredCv: (cv: StoredCvSummary) => void;
  onClearStoredCv: () => void;
  jobTitle: string;
  onJobTitleChange: (value: string) => void;
  jobOffer: string;
  onJobOfferChange: (value: string) => void;
  onSubmit: () => void;
  pending: boolean;
  maxUploadMb: number;
}

export function AnalyzerForm({
  cvFile,
  onCvChange,
  storedCvs,
  selectedCv,
  onSelectStoredCv,
  onClearStoredCv,
  jobTitle,
  onJobTitleChange,
  jobOffer,
  onJobOfferChange,
  onSubmit,
  pending,
  maxUploadMb,
}: AnalyzerFormProps) {
  const { t } = useI18n();
  const hasCv = cvFile !== null || selectedCv !== null;
  const canSubmit =
    !pending && hasCv && jobOffer.trim().length >= MIN_OFFER_LENGTH;

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        if (canSubmit) onSubmit();
      }}
      className="space-y-6 rounded-3xl border border-line bg-surface p-6 shadow-subtle"
    >
      <Field label={t.dashboard.cvLabel}>
        {selectedCv ? (
          <div className="flex items-center gap-3 rounded-xl border border-line bg-canvas px-4 py-3">
            <FileText className="h-5 w-5 shrink-0 text-ink-faint" aria-hidden />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">{selectedCv.filename}</p>
              <p className="text-xs text-ink-faint">{t.dashboard.reusedCvHint}</p>
            </div>
            <button
              type="button"
              onClick={onClearStoredCv}
              disabled={pending}
              aria-label={t.dashboard.removeFile}
              className="grid h-7 w-7 shrink-0 place-items-center rounded-lg text-ink-faint transition-colors hover:bg-surface hover:text-ink"
            >
              <X className="h-4 w-4" aria-hidden />
            </button>
          </div>
        ) : (
          <CvDropzone
            file={cvFile}
            onSelect={onCvChange}
            maxSizeMb={maxUploadMb}
            disabled={pending}
          />
        )}

        {!hasCv && storedCvs.length > 0 ? (
          <select
            value=""
            onChange={(event) => {
              const cv = storedCvs.find((item) => item.id === event.target.value);
              if (cv) onSelectStoredCv(cv);
            }}
            disabled={pending}
            aria-label={t.dashboard.reuseCvPlaceholder}
            className="mt-3 w-full rounded-xl border border-line bg-canvas px-4 py-2.5 text-sm text-ink-muted outline-none transition-colors focus:border-ink-faint"
          >
            <option value="" disabled>
              {t.dashboard.reuseCvPlaceholder}
            </option>
            {storedCvs.map((cv) => (
              <option key={cv.id} value={cv.id}>
                {cv.filename} · {formatDate(cv.createdAt)}
              </option>
            ))}
          </select>
        ) : null}
      </Field>

      <Field label={t.dashboard.roleLabel} hint={t.dashboard.optional}>
        <input
          type="text"
          value={jobTitle}
          onChange={(event) => onJobTitleChange(event.target.value)}
          placeholder={t.dashboard.rolePlaceholder}
          disabled={pending}
          className="w-full rounded-xl border border-line bg-canvas px-4 py-3 text-sm outline-none transition-colors placeholder:text-ink-faint focus:border-ink-faint"
        />
      </Field>

      <Field label={t.dashboard.jobLabel}>
        <textarea
          value={jobOffer}
          onChange={(event) => onJobOfferChange(event.target.value)}
          placeholder={t.dashboard.jobPlaceholder}
          rows={8}
          disabled={pending}
          className="w-full resize-none break-words rounded-xl border border-line bg-canvas px-4 py-3 text-sm leading-relaxed outline-none transition-colors placeholder:text-ink-faint focus:border-ink-faint"
        />
      </Field>

      <Button type="submit" disabled={!canSubmit} className="w-full" size="lg">
        {pending ? (
          <>
            <Spinner />
            {t.dashboard.analyzing}
          </>
        ) : (
          <>
            {t.dashboard.analyze}
            <ArrowRight className="h-4 w-4" />
          </>
        )}
      </Button>
    </form>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="mb-2 flex items-center justify-between text-sm font-medium">
        {label}
        {hint ? (
          <span className="text-xs font-normal text-ink-faint">{hint}</span>
        ) : null}
      </span>
      {children}
    </label>
  );
}
