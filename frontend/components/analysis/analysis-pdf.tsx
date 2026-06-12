import {
  Document,
  Page,
  StyleSheet,
  Text,
  View,
  pdf,
} from "@react-pdf/renderer";

import type { AnalysisViewData } from "@/components/analysis/analysis-view";
import type { Localized, Priority, Verdict } from "@/lib/types";

type Locale = keyof Localized;

export interface AnalysisPdfLabels {
  fit: string;
  summary: string;
  matched: string;
  missing: string;
  improve: string;
  matchedEmpty: string;
  missingEmpty: string;
  defaultTitle: string;
  tagline: string;
  verdict: Record<Verdict, string>;
  priority: Record<Priority, string>;
}

// The palette mirrors the app's light theme; @react-pdf can't read CSS
// variables, so the equivalents live here as hex.
const COLOR = {
  ink: "#181a1f",
  inkMuted: "#656a72",
  inkFaint: "#9b9ea4",
  line: "#e8e6e2",
  accent: "#0072e6",
  accentSoft: "#ebf4ff",
  positive: "#38895f",
  warning: "#c2700f",
  surface: "#ffffff",
};

const VERDICT_COLOR: Record<Verdict, string> = {
  strong: COLOR.accent,
  moderate: COLOR.inkMuted,
  weak: COLOR.warning,
};

const styles = StyleSheet.create({
  page: {
    paddingVertical: 48,
    paddingHorizontal: 48,
    fontFamily: "Helvetica",
    fontSize: 10,
    color: COLOR.ink,
    lineHeight: 1.5,
  },
  header: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "flex-start",
    borderBottomWidth: 1,
    borderBottomColor: COLOR.line,
    paddingBottom: 18,
    marginBottom: 22,
  },
  title: { fontSize: 20, fontFamily: "Helvetica-Bold", maxWidth: 320 },
  meta: { fontSize: 9, color: COLOR.inkFaint, marginTop: 4 },
  scoreBox: { alignItems: "flex-end" },
  score: { fontSize: 34, fontFamily: "Helvetica-Bold", color: COLOR.accent },
  verdict: { fontSize: 9, fontFamily: "Helvetica-Bold", marginTop: 2 },
  fitCaption: {
    fontSize: 7,
    color: COLOR.inkFaint,
    textTransform: "uppercase",
    letterSpacing: 1,
  },
  section: { marginBottom: 20 },
  sectionTitle: {
    fontSize: 8,
    fontFamily: "Helvetica-Bold",
    color: COLOR.inkFaint,
    textTransform: "uppercase",
    letterSpacing: 1,
    marginBottom: 8,
  },
  summary: { fontSize: 11, color: COLOR.ink, lineHeight: 1.6 },
  columns: { flexDirection: "row", gap: 24 },
  column: { flex: 1 },
  pillRow: { flexDirection: "row", flexWrap: "wrap", gap: 5 },
  pill: {
    borderWidth: 1,
    borderColor: COLOR.line,
    borderRadius: 8,
    paddingVertical: 3,
    paddingHorizontal: 7,
    fontSize: 9,
    color: COLOR.inkMuted,
  },
  emptyHint: { fontSize: 9, color: COLOR.inkFaint },
  suggestion: {
    borderWidth: 1,
    borderColor: COLOR.line,
    borderRadius: 10,
    padding: 12,
    marginBottom: 8,
  },
  suggestionHead: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "flex-start",
    gap: 8,
  },
  suggestionTitle: { fontSize: 11, fontFamily: "Helvetica-Bold", flex: 1 },
  priorityTag: {
    fontSize: 7,
    fontFamily: "Helvetica-Bold",
    color: COLOR.accent,
    backgroundColor: COLOR.accentSoft,
    borderRadius: 6,
    paddingVertical: 2,
    paddingHorizontal: 6,
  },
  suggestionDetail: { fontSize: 9.5, color: COLOR.inkMuted, marginTop: 5 },
  footer: {
    position: "absolute",
    bottom: 28,
    left: 48,
    right: 48,
    flexDirection: "row",
    justifyContent: "space-between",
    fontSize: 8,
    color: COLOR.inkFaint,
    borderTopWidth: 1,
    borderTopColor: COLOR.line,
    paddingTop: 8,
  },
});

interface DocProps {
  data: AnalysisViewData;
  locale: Locale;
  labels: AnalysisPdfLabels;
  dateText: string | null;
}

function SkillColumn({
  title,
  skills,
  emptyHint,
}: {
  title: string;
  skills: string[];
  emptyHint: string;
}) {
  return (
    <View style={styles.column}>
      <Text style={styles.sectionTitle}>{title}</Text>
      {skills.length === 0 ? (
        <Text style={styles.emptyHint}>{emptyHint}</Text>
      ) : (
        <View style={styles.pillRow}>
          {skills.map((skill, index) => (
            <Text key={index} style={styles.pill}>
              {skill}
            </Text>
          ))}
        </View>
      )}
    </View>
  );
}

function AnalysisPdfDoc({ data, locale, labels, dateText }: DocProps) {
  const title = data.jobTitle?.trim() || labels.defaultTitle;
  const metaParts = [data.cvFilename, dateText].filter(Boolean);

  return (
    <Document title={title} author="Papyrus">
      <Page size="A4" style={styles.page}>
        <View style={styles.header}>
          <View>
            <Text style={styles.title}>{title}</Text>
            {metaParts.length > 0 ? (
              <Text style={styles.meta}>{metaParts.join("  ·  ")}</Text>
            ) : null}
          </View>
          <View style={styles.scoreBox}>
            <Text style={styles.fitCaption}>{labels.fit}</Text>
            <Text style={styles.score}>{data.score}</Text>
            <Text
              style={[styles.verdict, { color: VERDICT_COLOR[data.verdict] }]}
            >
              {labels.verdict[data.verdict]}
            </Text>
          </View>
        </View>

        <View style={styles.section}>
          <Text style={styles.sectionTitle}>{labels.summary}</Text>
          <Text style={styles.summary}>{data.summary[locale]}</Text>
        </View>

        <View style={[styles.section, styles.columns]}>
          <SkillColumn
            title={labels.matched}
            skills={data.matchedSkills[locale]}
            emptyHint={labels.matchedEmpty}
          />
          <SkillColumn
            title={labels.missing}
            skills={data.missingSkills[locale]}
            emptyHint={labels.missingEmpty}
          />
        </View>

        {data.suggestions.length > 0 ? (
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{labels.improve}</Text>
            {data.suggestions.map((suggestion, index) => (
              <View key={index} style={styles.suggestion} wrap={false}>
                <View style={styles.suggestionHead}>
                  <Text style={styles.suggestionTitle}>
                    {suggestion.title[locale]}
                  </Text>
                  <Text style={styles.priorityTag}>
                    {labels.priority[suggestion.priority]}
                  </Text>
                </View>
                <Text style={styles.suggestionDetail}>
                  {suggestion.detail[locale]}
                </Text>
              </View>
            ))}
          </View>
        ) : null}

        <View style={styles.footer} fixed>
          <Text>{labels.tagline}</Text>
          <Text
            render={({ pageNumber, totalPages }) =>
              `${pageNumber} / ${totalPages}`
            }
          />
        </View>
      </Page>
    </Document>
  );
}

/** Builds the PDF in the browser and triggers a download. */
export async function downloadAnalysisPdf(
  data: AnalysisViewData,
  locale: Locale,
  labels: AnalysisPdfLabels,
  dateText: string | null,
  filename: string,
): Promise<void> {
  const blob = await pdf(
    <AnalysisPdfDoc
      data={data}
      locale={locale}
      labels={labels}
      dateText={dateText}
    />,
  ).toBlob();

  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
