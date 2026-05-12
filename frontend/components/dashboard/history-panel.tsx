"use client";

import { Trash2 } from "lucide-react";
import { useState } from "react";

import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useI18n } from "@/lib/i18n/context";
import type { AnalysisRecord } from "@/lib/types";
import { cn, formatDate } from "@/lib/utils";

interface HistoryPanelProps {
  records: AnalysisRecord[];
  activeId: string | null;
  onSelect: (record: AnalysisRecord) => void;
  onDelete: (record: AnalysisRecord) => void;
  loading: boolean;
}

export function HistoryPanel({
  records,
  activeId,
  onSelect,
  onDelete,
  loading,
}: HistoryPanelProps) {
  const { t } = useI18n();
  const [pendingDelete, setPendingDelete] = useState<AnalysisRecord | null>(null);

  function confirmDelete() {
    if (pendingDelete) {
      onDelete(pendingDelete);
    }
    setPendingDelete(null);
  }

  return (
    <section>
      <h2 className="text-xs font-medium uppercase tracking-[0.12em] text-ink-faint">
        {t.dashboard.historyTitle}
      </h2>

      {loading ? (
        <p className="mt-4 text-sm text-ink-faint">{t.dashboard.historyLoading}</p>
      ) : records.length === 0 ? (
        <p className="mt-4 text-sm text-ink-faint">{t.dashboard.historyEmpty}</p>
      ) : (
        <ul className="mt-4 space-y-1">
          {records.map((record) => (
            <li key={record.id} className="group flex items-center gap-1">
              <button
                type="button"
                onClick={() => onSelect(record)}
                className={cn(
                  "flex min-w-0 flex-1 items-center gap-3 rounded-xl border px-3.5 py-3 text-left transition-colors",
                  record.id === activeId
                    ? "border-line bg-surface shadow-subtle"
                    : "border-transparent hover:bg-surface",
                )}
              >
                <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-accent-soft text-sm font-semibold tabular-nums text-accent">
                  {record.score}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">
                    {record.jobTitle?.trim() || t.dashboard.untitledRole}
                  </span>
                  <span className="block text-xs text-ink-faint">
                    {formatDate(record.createdAt)}
                  </span>
                </span>
              </button>
              <button
                type="button"
                onClick={() => setPendingDelete(record)}
                aria-label={t.dashboard.deleteAnalysis}
                className="grid h-8 w-8 shrink-0 place-items-center rounded-lg text-ink-faint opacity-0 transition-all hover:bg-danger/10 hover:text-danger focus-visible:opacity-100 group-hover:opacity-100"
              >
                <Trash2 className="h-4 w-4" aria-hidden />
              </button>
            </li>
          ))}
        </ul>
      )}

      <ConfirmDialog
        open={pendingDelete !== null}
        title={t.dashboard.confirmDelete.title}
        description={t.dashboard.confirmDelete.description}
        confirmLabel={t.dashboard.confirmDelete.confirm}
        cancelLabel={t.dashboard.confirmDelete.cancel}
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete(null)}
      />
    </section>
  );
}
