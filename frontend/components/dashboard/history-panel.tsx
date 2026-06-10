"use client";

import { ChevronDown, Trash2 } from "lucide-react";
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
  // On mobile the list sits below a tall form, so it stays collapsed by default
  // to avoid a long scroll. Desktop always shows it (the toggle is mobile-only).
  const [expanded, setExpanded] = useState(false);

  const hasRecords = !loading && records.length > 0;

  function confirmDelete() {
    if (pendingDelete) {
      onDelete(pendingDelete);
    }
    setPendingDelete(null);
  }

  return (
    <section>
      {hasRecords ? (
        <button
          type="button"
          onClick={() => setExpanded((value) => !value)}
          aria-expanded={expanded}
          className="flex w-full items-center justify-between gap-2 lg:cursor-default"
        >
          <span className="flex items-center gap-2 text-xs font-medium uppercase tracking-[0.12em] text-ink-faint">
            {t.dashboard.historyTitle}
            <span className="rounded-full border border-line px-1.5 py-0.5 text-[10px] tabular-nums leading-none">
              {records.length}
            </span>
          </span>
          <ChevronDown
            className={cn(
              "h-4 w-4 shrink-0 text-ink-faint transition-transform lg:hidden",
              expanded && "rotate-180",
            )}
            aria-hidden
          />
        </button>
      ) : (
        <h2 className="text-xs font-medium uppercase tracking-[0.12em] text-ink-faint">
          {t.dashboard.historyTitle}
        </h2>
      )}

      {loading ? (
        <p className="mt-4 text-sm text-ink-faint">{t.dashboard.historyLoading}</p>
      ) : records.length === 0 ? (
        <p className="mt-4 text-sm text-ink-faint">{t.dashboard.historyEmpty}</p>
      ) : (
        <ul className={cn("mt-4 space-y-1", !expanded && "hidden lg:block")}>
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
