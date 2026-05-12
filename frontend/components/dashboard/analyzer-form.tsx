"use client";

import { ArrowRight } from "lucide-react";

import { CvDropzone } from "@/components/dashboard/cv-dropzone";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/lib/i18n/context";

const MIN_OFFER_LENGTH = 40;

interface AnalyzerFormProps {
  cvFile: File | null;
  onCvChange: (file: File | null) => void;
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
  jobTitle,
  onJobTitleChange,
  jobOffer,
  onJobOfferChange,
  onSubmit,
  pending,
  maxUploadMb,
}: AnalyzerFormProps) {
  const { t } = useI18n();
  const canSubmit =
    !pending && cvFile !== null && jobOffer.trim().length >= MIN_OFFER_LENGTH;

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        if (canSubmit) onSubmit();
      }}
      className="space-y-6 rounded-3xl border border-line bg-surface p-6 shadow-subtle"
    >
      <Field label={t.dashboard.cvLabel}>
        <CvDropzone
          file={cvFile}
          onSelect={onCvChange}
          maxSizeMb={maxUploadMb}
          disabled={pending}
        />
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
          className="w-full resize-none rounded-xl border border-line bg-canvas px-4 py-3 text-sm leading-relaxed outline-none transition-colors placeholder:text-ink-faint focus:border-ink-faint"
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
