"use client";

import { ArrowLeft } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import {
  AnalysisView,
  type AnalysisViewData,
} from "@/components/analysis/analysis-view";
import { AnalyzerForm } from "@/components/dashboard/analyzer-form";
import { HistoryPanel } from "@/components/dashboard/history-panel";
import {
  AnalyzingState,
  CvNotice,
  EmptyState,
  ErrorState,
} from "@/components/dashboard/result-states";
import {
  deleteAnalysis,
  listAnalyses,
  saveAnalysis,
} from "@/lib/analyses/repository";
import { GatewayError, localizeGatewayError, requestAnalysis } from "@/lib/api/gateway";
import {
  createCvDownloadUrl,
  downloadStoredCv,
  listCvs,
  removeStoredCv,
  uploadCv,
  type StoredCvSummary,
} from "@/lib/cvs/repository";
import { MAX_UPLOAD_MB } from "@/lib/constants";
import { useI18n } from "@/lib/i18n/context";
import { createClient } from "@/lib/supabase/client";
import type { AnalysisRecord } from "@/lib/types";
import { cn } from "@/lib/utils";

type Status = "idle" | "analyzing" | "ready" | "error";

// On mobile the form and the result share one column, so we treat them as two
// screens and surface only one at a time. Desktop always shows both.
type MobileView = "form" | "result";

function scrollToTopOnMobile() {
  if (typeof window === "undefined") return;
  if (window.matchMedia("(min-width: 1024px)").matches) return;
  window.scrollTo({ top: 0, behavior: "smooth" });
}

interface ActiveAnalysis {
  id: string;
  data: AnalysisViewData;
  cvStoragePath: string | null;
}

function toView(record: AnalysisRecord): AnalysisViewData {
  return {
    jobTitle: record.jobTitle,
    cvFilename: record.cvFilename,
    score: record.score,
    verdict: record.verdict,
    summary: record.summary,
    matchedSkills: record.matchedSkills,
    missingSkills: record.missingSkills,
    suggestions: record.suggestions,
    createdAt: record.createdAt,
  };
}

export function Workspace({ userId }: { userId: string }) {
  const { t } = useI18n();
  const supabase = useMemo(() => createClient(), []);

  const [cvFile, setCvFile] = useState<File | null>(null);
  const [selectedCv, setSelectedCv] = useState<StoredCvSummary | null>(null);
  const [storedCvs, setStoredCvs] = useState<StoredCvSummary[]>([]);
  const [jobTitle, setJobTitle] = useState("");
  const [jobOffer, setJobOffer] = useState("");

  const [status, setStatus] = useState<Status>("idle");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [cvNotice, setCvNotice] = useState<string | null>(null);
  const [active, setActive] = useState<ActiveAnalysis | null>(null);
  const [mobileView, setMobileView] = useState<MobileView>("form");

  const [history, setHistory] = useState<AnalysisRecord[]>([]);
  const [historyLoading, setHistoryLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    listAnalyses(supabase, userId)
      .then((records) => {
        if (!cancelled) setHistory(records);
      })
      .catch((error) => {
        if (!cancelled) console.error("Failed to load history:", error);
      })
      .finally(() => {
        if (!cancelled) setHistoryLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [supabase, userId]);

  useEffect(() => {
    let cancelled = false;
    listCvs(supabase, userId)
      .then((cvs) => {
        if (!cancelled) setStoredCvs(cvs);
      })
      .catch((error) => {
        if (!cancelled) console.error("Failed to load CVs:", error);
      });
    return () => {
      cancelled = true;
    };
  }, [supabase, userId]);

  // Choosing a freshly uploaded file and reusing a saved one are mutually
  // exclusive, so picking either clears the other.
  function handleCvChange(file: File | null) {
    setCvFile(file);
    if (file) setSelectedCv(null);
  }

  function selectStoredCv(cv: StoredCvSummary) {
    setSelectedCv(cv);
    setCvFile(null);
  }

  async function handleAnalyze() {
    if (!cvFile && !selectedCv) return;

    setStatus("analyzing");
    setErrorMessage(null);
    setCvNotice(null);
    setActive(null);
    setMobileView("result");
    scrollToTopOnMobile();

    try {
      const {
        data: { session },
      } = await supabase.auth.getSession();
      if (!session) {
        throw new GatewayError(t.result.sessionExpired, "unauthorized", 401);
      }

      // A reused CV is already in storage: pull the bytes back to re-analyze it
      // and link the existing record instead of uploading a duplicate.
      let cvForAnalysis: File;
      let cvId: string | undefined;
      if (selectedCv) {
        const blob = await downloadStoredCv(supabase, selectedCv.storagePath);
        cvForAnalysis = new File([blob], selectedCv.filename, {
          type: "application/pdf",
        });
        cvId = selectedCv.id;
      } else {
        cvForAnalysis = cvFile!;
      }

      const normalizedTitle = jobTitle.trim() || undefined;
      const result = await requestAnalysis({
        cv: cvForAnalysis,
        jobOffer,
        jobTitle: normalizedTitle,
        accessToken: session.access_token,
      });

      // Persisting a freshly uploaded CV is best-effort: a storage hiccup must
      // not cost the user the analysis they just ran.
      if (!cvId) {
        try {
          const stored = await uploadCv(supabase, userId, cvForAnalysis);
          cvId = stored.id;
          setStoredCvs((previous) => [
            {
              id: stored.id,
              filename: cvForAnalysis.name,
              storagePath: stored.storagePath,
              createdAt: new Date().toISOString(),
            },
            ...previous.filter((cv) => cv.filename !== cvForAnalysis.name),
          ]);
        } catch (storageError) {
          console.error("CV upload failed; saving analysis without it:", storageError);
          setCvNotice(t.result.cvNotice);
        }
      }

      const record = await saveAnalysis(supabase, userId, {
        result,
        jobOffer,
        jobTitle: normalizedTitle,
        cvId,
      });

      setHistory((previous) => [record, ...previous]);
      setActive({
        id: record.id,
        data: toView(record),
        cvStoragePath: record.cvStoragePath,
      });
      setStatus("ready");
    } catch (error) {
      setStatus("error");
      setErrorMessage(
        error instanceof GatewayError
          ? localizeGatewayError(error, t, MAX_UPLOAD_MB)
          : t.result.errorGeneric,
      );
    }
  }

  function selectRecord(record: AnalysisRecord) {
    setCvNotice(null);
    setActive({
      id: record.id,
      data: toView(record),
      cvStoragePath: record.cvStoragePath,
    });
    setStatus("ready");
    setMobileView("result");
    scrollToTopOnMobile();
  }

  function backToForm() {
    setMobileView("form");
    scrollToTopOnMobile();
  }

  async function downloadCv(storagePath: string) {
    try {
      const url = await createCvDownloadUrl(supabase, storagePath);
      window.open(url, "_blank", "noopener");
    } catch (error) {
      console.error("Failed to open CV:", error);
    }
  }

  async function handleDelete(record: AnalysisRecord) {
    setHistory((previous) => previous.filter((item) => item.id !== record.id));
    if (active?.id === record.id) {
      setActive(null);
      setStatus("idle");
      setMobileView("form");
    }

    try {
      await deleteAnalysis(supabase, record.id);
      if (record.cvStoragePath) {
        await removeStoredCv(supabase, record.cvStoragePath);
      }
    } catch (error) {
      console.error("Failed to delete analysis:", error);
      // Restore the row and keep the list ordered by recency.
      setHistory((previous) =>
        [record, ...previous].sort((a, b) =>
          b.createdAt.localeCompare(a.createdAt),
        ),
      );
    }
  }

  let downloadHandler: (() => void) | undefined;
  if (active?.cvStoragePath) {
    const path = active.cvStoragePath;
    downloadHandler = () => downloadCv(path);
  }

  return (
    <div className="mx-auto max-w-content px-6 py-10 lg:py-14">
      <div className="mb-10 hidden lg:block">
        <h1 className="text-2xl font-semibold tracking-tight">
          {t.dashboard.title}
        </h1>
        <p className="mt-1 text-sm text-ink-muted">{t.dashboard.subtitle}</p>
      </div>

      <div className="grid gap-10 lg:grid-cols-[380px_1fr]">
        <div
          className={cn(
            "min-w-0 space-y-10",
            mobileView === "result" && "hidden lg:block",
          )}
        >
          <AnalyzerForm
            cvFile={cvFile}
            onCvChange={handleCvChange}
            storedCvs={storedCvs}
            selectedCv={selectedCv}
            onSelectStoredCv={selectStoredCv}
            onClearStoredCv={() => setSelectedCv(null)}
            jobTitle={jobTitle}
            onJobTitleChange={setJobTitle}
            jobOffer={jobOffer}
            onJobOfferChange={setJobOffer}
            onSubmit={handleAnalyze}
            pending={status === "analyzing"}
            maxUploadMb={MAX_UPLOAD_MB}
          />

          <HistoryPanel
            records={history}
            activeId={active?.id ?? null}
            onSelect={selectRecord}
            onDelete={handleDelete}
            loading={historyLoading}
          />
        </div>

        <div
          className={cn(
            "min-w-0",
            mobileView === "form" && "hidden lg:block",
          )}
        >
          <button
            type="button"
            onClick={backToForm}
            className="mb-5 inline-flex items-center gap-1.5 text-sm font-medium text-ink-muted transition-colors hover:text-ink lg:hidden"
          >
            <ArrowLeft className="h-4 w-4" aria-hidden />
            {t.result.back}
          </button>
          {status === "analyzing" ? (
            <AnalyzingState />
          ) : status === "error" ? (
            <ErrorState message={errorMessage ?? t.result.errorGeneric} />
          ) : active ? (
            <div className="space-y-5">
              {cvNotice ? <CvNotice message={cvNotice} /> : null}
              <AnalysisView data={active.data} onDownloadCv={downloadHandler} />
            </div>
          ) : (
            <EmptyState />
          )}
        </div>
      </div>
    </div>
  );
}
