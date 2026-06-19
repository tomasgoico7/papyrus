"use client";

import { ArrowLeft, FileDown, FileText, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import type { CvLabels } from "@/components/analysis/cv-pdf";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import {
  generateTailoredCv,
  requestTailoringQuestions,
} from "@/lib/api/tailor";
import { useI18n } from "@/lib/i18n/context";
import { createClient } from "@/lib/supabase/client";
import type { TailoredCv, TailoringAnswer, TailoringQuestion } from "@/lib/types";
import { cn, cvFileName } from "@/lib/utils";

type Step = "loading" | "questions" | "generating" | "result" | "error";

interface TailorDialogProps {
  open: boolean;
  getCv: () => Promise<File>;
  jobOffer: string;
  jobTitle: string | null;
  savedCv: TailoredCv | null;
  onSaved: (cv: TailoredCv) => void;
  onClose: () => void;
}

export function TailorDialog({
  open,
  getCv,
  jobOffer,
  jobTitle,
  savedCv,
  onSaved,
  onClose,
}: TailorDialogProps) {
  const { t, locale } = useI18n();
  const supabase = useMemo(() => createClient(), []);
  const startedRef = useRef(false);

  const [step, setStep] = useState<Step>("loading");
  const [questions, setQuestions] = useState<TailoringQuestion[]>([]);
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [skipped, setSkipped] = useState<Record<string, boolean>>({});
  const [extra, setExtra] = useState("");
  const [cvText, setCvText] = useState("");
  const [cv, setCv] = useState<TailoredCv | null>(null);
  const [questionsLoaded, setQuestionsLoaded] = useState(false);
  const [busyFormat, setBusyFormat] = useState<"pdf" | "docx" | null>(null);

  async function accessToken(): Promise<string> {
    const {
      data: { session },
    } = await supabase.auth.getSession();
    if (!session) throw new Error("no session");
    return session.access_token;
  }

  async function startQuestions() {
    setStep("loading");
    try {
      const cvFile = await getCv();
      const result = await requestTailoringQuestions({
        cv: cvFile,
        jobOffer,
        jobTitle: jobTitle ?? undefined,
        locale,
        accessToken: await accessToken(),
      });
      setQuestions(result.questions);
      setCvText(result.cvText);
      setQuestionsLoaded(true);
      setStep("questions");
    } catch (error) {
      console.error("Failed to prepare tailoring:", error);
      setStep("error");
    }
  }

  // On open: a previously saved CV is shown straight away (view / download /
  // re-adapt); otherwise we go fetch the tailoring questions.
  useEffect(() => {
    if (!open) {
      startedRef.current = false;
      return;
    }
    if (startedRef.current) return;
    startedRef.current = true;

    setQuestions([]);
    setAnswers({});
    setSkipped({});
    setExtra("");
    setQuestionsLoaded(false);

    if (savedCv) {
      setCv(savedCv);
      setStep("result");
    } else {
      setCv(null);
      void startQuestions();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKeyDown);
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.body.style.overflow = previous;
    };
  }, [open, onClose]);

  if (!open) return null;

  async function handleGenerate() {
    setStep("generating");
    try {
      const used: TailoringAnswer[] = questions
        .filter((q) => !skipped[q.topic] && (answers[q.topic] ?? "").trim())
        .map((q) => ({ topic: q.topic, answer: (answers[q.topic] ?? "").trim() }));

      const result = await generateTailoredCv({
        cvText,
        jobOffer,
        jobTitle: jobTitle ?? undefined,
        answers: used,
        extra: extra.trim() || undefined,
        accessToken: await accessToken(),
      });
      setCv(result);
      setStep("result");
      onSaved(result);
    } catch (error) {
      console.error("Failed to generate CV:", error);
      setStep("error");
    }
  }

  async function handleDownload(format: "pdf" | "docx") {
    if (!cv) return;
    setBusyFormat(format);
    try {
      const labels: CvLabels = {
        summary: t.tailor.secSummary,
        experience: t.tailor.secExperience,
        skills: t.tailor.secSkills,
        education: t.tailor.secEducation,
      };
      const base = cvFileName(cv.fullName);
      if (format === "pdf") {
        const { downloadTailoredCvPdf } = await import(
          "@/components/analysis/cv-pdf"
        );
        await downloadTailoredCvPdf(cv, labels, `${base}.pdf`);
      } else {
        const { downloadTailoredCvDocx } = await import(
          "@/components/analysis/cv-docx"
        );
        await downloadTailoredCvDocx(cv, labels, `${base}.docx`);
      }
    } catch (error) {
      console.error("Failed to export CV:", error);
    } finally {
      setBusyFormat(null);
    }
  }

  const wide = step === "result";

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
        aria-labelledby="tailor-dialog-title"
        className={cn(
          "relative flex max-h-[88vh] w-full animate-fade-up flex-col rounded-2xl border border-line bg-surface shadow-lifted",
          wide ? "max-w-3xl" : "max-w-2xl",
        )}
      >
        <div className="flex shrink-0 items-center justify-between gap-4 border-b border-line px-6 py-4">
          <h2 id="tailor-dialog-title" className="text-lg font-medium">
            {t.tailor.dialogTitle}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label={t.tailor.close}
            className="-mr-1 grid h-8 w-8 shrink-0 place-items-center rounded-lg text-ink-faint transition-colors hover:bg-canvas hover:text-ink"
          >
            <X className="h-4 w-4" aria-hidden />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto scrollbar-thin px-6 py-5">
          {step === "loading" ? (
            <Centered>
              <Spinner className="h-6 w-6 text-accent" />
              <p className="mt-4 text-sm text-ink-muted">
                {t.tailor.loadingQuestions}
              </p>
            </Centered>
          ) : step === "generating" ? (
            <Centered>
              <Spinner className="h-6 w-6 text-accent" />
              <p className="mt-4 text-sm text-ink-muted">{t.tailor.generating}</p>
            </Centered>
          ) : step === "error" ? (
            <Centered>
              <p className="text-sm text-ink-muted">{t.tailor.error}</p>
            </Centered>
          ) : step === "questions" ? (
            <QuestionsStep
              questions={questions}
              answers={answers}
              skipped={skipped}
              extra={extra}
              onAnswer={(topic, value) =>
                setAnswers((prev) => ({ ...prev, [topic]: value }))
              }
              onSkip={(topic, value) =>
                setSkipped((prev) => ({ ...prev, [topic]: value }))
              }
              onExtra={setExtra}
            />
          ) : cv ? (
            <CvPreview cv={cv} />
          ) : null}
        </div>

        {step === "questions" ? (
          <div className="flex shrink-0 justify-end border-t border-line px-6 py-4">
            <Button type="button" onClick={handleGenerate}>
              {t.tailor.generate}
            </Button>
          </div>
        ) : step === "result" && cv ? (
          <div className="flex shrink-0 items-center justify-between gap-3 border-t border-line px-6 py-4">
            <button
              type="button"
              onClick={questionsLoaded ? () => setStep("questions") : startQuestions}
              className="inline-flex items-center gap-1.5 text-sm font-medium text-ink-muted transition-colors hover:text-ink"
            >
              <ArrowLeft className="h-4 w-4" aria-hidden />
              {questionsLoaded ? t.tailor.back : t.tailor.readapt}
            </button>
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                disabled={busyFormat !== null}
                onClick={() => handleDownload("docx")}
              >
                {busyFormat === "docx" ? (
                  <Spinner className="h-4 w-4" />
                ) : (
                  <FileText className="h-4 w-4" aria-hidden />
                )}
                {t.tailor.downloadDocx}
              </Button>
              <Button
                type="button"
                size="sm"
                disabled={busyFormat !== null}
                onClick={() => handleDownload("pdf")}
              >
                {busyFormat === "pdf" ? (
                  <Spinner className="h-4 w-4" />
                ) : (
                  <FileDown className="h-4 w-4" aria-hidden />
                )}
                {t.tailor.downloadPdf}
              </Button>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid min-h-[16rem] place-items-center text-center">
      <div className="flex flex-col items-center">{children}</div>
    </div>
  );
}

interface QuestionsStepProps {
  questions: TailoringQuestion[];
  answers: Record<string, string>;
  skipped: Record<string, boolean>;
  extra: string;
  onAnswer: (topic: string, value: string) => void;
  onSkip: (topic: string, value: boolean) => void;
  onExtra: (value: string) => void;
}

function QuestionsStep({
  questions,
  answers,
  skipped,
  extra,
  onAnswer,
  onSkip,
  onExtra,
}: QuestionsStepProps) {
  const { t } = useI18n();
  return (
    <div className="space-y-6">
      <p className="text-sm leading-relaxed text-ink-muted">{t.tailor.step1Intro}</p>

      {questions.length === 0 ? (
        <p className="rounded-xl border border-line bg-canvas px-4 py-3 text-sm text-ink-muted">
          {t.tailor.emptyQuestions}
        </p>
      ) : (
        <ul className="space-y-5">
          {questions.map((question) => {
            const isSkipped = skipped[question.topic] ?? false;
            return (
              <li key={question.topic}>
                <p className="text-sm font-medium">{question.question}</p>
                <textarea
                  value={answers[question.topic] ?? ""}
                  onChange={(event) =>
                    onAnswer(question.topic, event.target.value)
                  }
                  disabled={isSkipped}
                  rows={2}
                  placeholder={t.tailor.answerPlaceholder}
                  className="mt-2 w-full resize-none rounded-xl border border-line bg-canvas px-4 py-2.5 text-sm outline-none transition-colors placeholder:text-ink-faint focus:border-ink-faint disabled:opacity-50"
                />
                <label className="mt-2 inline-flex cursor-pointer items-center gap-2 text-xs text-ink-faint">
                  <input
                    type="checkbox"
                    checked={isSkipped}
                    onChange={(event) =>
                      onSkip(question.topic, event.target.checked)
                    }
                    className="h-3.5 w-3.5 rounded border-line accent-accent"
                  />
                  {t.tailor.noHave}
                </label>
              </li>
            );
          })}
        </ul>
      )}

      <div>
        <label className="block text-sm font-medium">{t.tailor.extraLabel}</label>
        <textarea
          value={extra}
          onChange={(event) => onExtra(event.target.value)}
          rows={2}
          placeholder={t.tailor.extraPlaceholder}
          className="mt-2 w-full resize-none rounded-xl border border-line bg-canvas px-4 py-2.5 text-sm outline-none transition-colors placeholder:text-ink-faint focus:border-ink-faint"
        />
      </div>
    </div>
  );
}

function CvPreview({ cv }: { cv: TailoredCv }) {
  const { t } = useI18n();
  return (
    <article className="space-y-6">
      <p className="rounded-xl border border-line bg-canvas px-4 py-2.5 text-xs leading-relaxed text-ink-muted">
        {t.tailor.resultNote}
      </p>

      <div className="rounded-2xl border border-line bg-canvas p-6">
        <header>
          <h3 className="text-2xl font-semibold tracking-tight">{cv.fullName}</h3>
          {cv.headline ? (
            <p className="mt-2 text-sm font-medium text-accent">{cv.headline}</p>
          ) : null}
          {cv.contact ? (
            <p className="mt-2.5 text-xs leading-relaxed text-ink-faint">
              {cv.contact}
            </p>
          ) : null}
        </header>

        {cv.summary ? (
          <Section title={t.tailor.secSummary}>
            <p className="text-sm leading-relaxed text-ink-muted">{cv.summary}</p>
          </Section>
        ) : null}

        {cv.experience.length > 0 ? (
          <Section title={t.tailor.secExperience}>
            <div className="space-y-4">
              {cv.experience.map((item, index) => (
                <div key={index}>
                  <div className="flex items-baseline justify-between gap-3">
                    <p className="text-sm font-medium">{item.role}</p>
                    <p className="shrink-0 text-xs text-ink-faint">{item.period}</p>
                  </div>
                  {item.company ? (
                    <p className="text-xs text-ink-muted">{item.company}</p>
                  ) : null}
                  <ul className="mt-1.5 space-y-1">
                    {item.highlights.map((highlight, hi) => (
                      <li
                        key={hi}
                        className="flex gap-2 text-sm leading-relaxed text-ink-muted"
                      >
                        <span className="text-ink-faint">•</span>
                        <span className="min-w-0">{highlight}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </Section>
        ) : null}

        {cv.skills.length > 0 ? (
          <Section title={t.tailor.secSkills}>
            <div className="space-y-1">
              {cv.skills.map((group, index) => {
                const split = group.indexOf(":");
                return (
                  <p
                    key={index}
                    className="text-sm leading-relaxed text-ink-muted"
                  >
                    {split === -1 ? (
                      group
                    ) : (
                      <>
                        <span className="font-medium text-ink">
                          {group.slice(0, split + 1)}
                        </span>
                        {group.slice(split + 1)}
                      </>
                    )}
                  </p>
                );
              })}
            </div>
          </Section>
        ) : null}

        {cv.education.length > 0 ? (
          <Section title={t.tailor.secEducation}>
            <div className="space-y-1.5">
              {cv.education.map((item, index) => (
                <div
                  key={index}
                  className="flex items-baseline justify-between gap-3 text-sm"
                >
                  <p className="min-w-0">
                    {item.degree}
                    {item.institution ? (
                      <span className="text-ink-muted"> — {item.institution}</span>
                    ) : null}
                  </p>
                  <p className="shrink-0 text-xs text-ink-faint">{item.period}</p>
                </div>
              ))}
            </div>
          </Section>
        ) : null}

        {cv.additional.map((sec, index) =>
          sec.items.length > 0 ? (
            <Section key={index} title={sec.title}>
              <div className="space-y-1">
                {sec.items.map((entry, ei) => (
                  <p key={ei} className="text-sm leading-relaxed text-ink-muted">
                    {entry}
                  </p>
                ))}
              </div>
            </Section>
          ) : null,
        )}
      </div>
    </article>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className="mt-5 border-t border-line pt-4">
      <h4 className="mb-2 text-xs font-medium uppercase tracking-[0.12em] text-ink-faint">
        {title}
      </h4>
      {children}
    </section>
  );
}
