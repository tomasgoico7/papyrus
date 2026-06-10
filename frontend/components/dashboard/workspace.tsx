"use client";

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
import { createCvDownloadUrl, removeStoredCv, uploadCv } from "@/lib/cvs/repository";
import { MAX_UPLOAD_MB } from "@/lib/constants";
import { useI18n } from "@/lib/i18n/context";
import { createClient } from "@/lib/supabase/client";
import type { AnalysisRecord } from "@/lib/types";

type Status = "idle" | "analyzing" | "ready" | "error";

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
  const [jobTitle, setJobTitle] = useState("");
  const [jobOffer, setJobOffer] = useState("");

  const [status, setStatus] = useState<Status>("idle");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [cvNotice, setCvNotice] = useState<string | null>(null);
  const [active, setActive] = useState<ActiveAnalysis | null>(null);

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

  async function handleAnalyze() {
    if (!cvFile) return;

    setStatus("analyzing");
    setErrorMessage(null);
    setCvNotice(null);
    setActive(null);

    try {
      const {
        data: { session },
      } = await supabase.auth.getSession();
      if (!session) {
        throw new GatewayError(t.result.sessionExpired, "unauthorized", 401);
      }

      const normalizedTitle = jobTitle.trim() || undefined;
      const result = await requestAnalysis({
        cv: cvFile,
        jobOffer,
        jobTitle: normalizedTitle,
        accessToken: session.access_token,
      });

      // Persisting the CV is best-effort: a storage hiccup must not cost the
      // user the analysis they just ran.
      let cvId: string | undefined;
      try {
        cvId = (await uploadCv(supabase, userId, cvFile)).id;
      } catch (storageError) {
        console.error("CV upload failed; saving analysis without it:", storageError);
        setCvNotice(t.result.cvNotice);
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
      <div className="mb-10">
        <h1 className="text-2xl font-semibold tracking-tight">
          {t.dashboard.title}
        </h1>
        <p className="mt-1 text-sm text-ink-muted">{t.dashboard.subtitle}</p>
      </div>

      <div className="grid gap-10 lg:grid-cols-[380px_1fr]">
        <div className="space-y-10">
          <AnalyzerForm
            cvFile={cvFile}
            onCvChange={setCvFile}
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

        <div>
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
