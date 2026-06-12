import {
  Document,
  Font,
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

// Helvetica hyphenates long words by default, which mangles job titles.
Font.registerHyphenationCallback((word) => [word]);

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
  warningSoft: "#fbf0e1",
  neutralSoft: "#f2f0ec",
};

const VERDICT_STYLE: Record<Verdict, { color: string; background: string }> = {
  strong: { color: COLOR.accent, background: COLOR.accentSoft },
  moderate: { color: COLOR.inkMuted, background: COLOR.neutralSoft },
  weak: { color: COLOR.warning, background: COLOR.warningSoft },
};

const PRIORITY_STYLE: Record<Priority, { color: string; background: string }> = {
  high: { color: COLOR.warning, background: COLOR.warningSoft },
  medium: { color: COLOR.accent, background: COLOR.accentSoft },
  low: { color: COLOR.inkMuted, background: COLOR.neutralSoft },
};

const styles = StyleSheet.create({
  page: {
    paddingTop: 48,
    paddingBottom: 64,
    paddingHorizontal: 52,
    fontFamily: "Helvetica",
    fontSize: 10,
    color: COLOR.ink,
    lineHeight: 1.5,
  },
  header: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "flex-start",
  },
  headerLeft: { flex: 1, paddingRight: 28 },
  verdictPill: {
    alignSelf: "flex-start",
    borderRadius: 10,
    paddingVertical: 3,
    paddingHorizontal: 9,
    fontSize: 8.5,
    fontFamily: "Helvetica-Bold",
  },
  title: {
    marginTop: 10,
    fontSize: 21,
    fontFamily: "Helvetica-Bold",
    lineHeight: 1.25,
  },
  meta: { marginTop: 6, fontSize: 9, color: COLOR.inkFaint },
  scoreCard: {
    width: 104,
    alignItems: "center",
    borderRadius: 14,
    backgroundColor: COLOR.accentSoft,
    paddingVertical: 14,
    paddingHorizontal: 12,
  },
  scoreCaption: {
    fontSize: 7,
    color: COLOR.inkMuted,
    textTransform: "uppercase",
    letterSpacing: 1.2,
  },
  scoreRow: { flexDirection: "row", alignItems: "flex-end", marginTop: 4 },
  scoreValue: {
    fontSize: 30,
    fontFamily: "Helvetica-Bold",
    color: COLOR.accent,
    lineHeight: 1,
  },
  scoreOutOf: { fontSize: 9, color: COLOR.inkFaint, marginBottom: 3, marginLeft: 2 },
  divider: {
    borderBottomWidth: 1,
    borderBottomColor: COLOR.line,
    marginTop: 24,
    marginBottom: 24,
  },
  section: { marginBottom: 24 },
  sectionTitle: {
    fontSize: 8,
    fontFamily: "Helvetica-Bold",
    color: COLOR.inkFaint,
    textTransform: "uppercase",
    letterSpacing: 1.2,
    marginBottom: 10,
  },
  summary: { fontSize: 11, lineHeight: 1.65 },
  columns: { flexDirection: "row" },
  column: { flex: 1 },
  columnGap: { width: 32 },
  skillRow: {
    flexDirection: "row",
    alignItems: "flex-start",
    marginBottom: 6,
    paddingRight: 8,
  },
  skillDot: {
    width: 5,
    height: 5,
    borderRadius: 2.5,
    marginTop: 4.5,
    marginRight: 7,
  },
  skillText: { flex: 1, fontSize: 9.5, color: COLOR.inkMuted, lineHeight: 1.45 },
  emptyHint: { fontSize: 9, color: COLOR.inkFaint },
  suggestion: {
    borderWidth: 1,
    borderColor: COLOR.line,
    borderRadius: 12,
    padding: 14,
    marginBottom: 10,
  },
  suggestionHead: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "flex-start",
  },
  suggestionTitle: {
    flex: 1,
    paddingRight: 12,
    fontSize: 11,
    fontFamily: "Helvetica-Bold",
    lineHeight: 1.35,
  },
  priorityTag: {
    borderRadius: 8,
    paddingVertical: 2.5,
    paddingHorizontal: 7,
    fontSize: 7.5,
    fontFamily: "Helvetica-Bold",
  },
  suggestionDetail: {
    marginTop: 6,
    fontSize: 9.5,
    color: COLOR.inkMuted,
    lineHeight: 1.55,
  },
  footer: {
    position: "absolute",
    bottom: 30,
    left: 52,
    right: 52,
    flexDirection: "row",
    justifyContent: "space-between",
    borderTopWidth: 1,
    borderTopColor: COLOR.line,
    paddingTop: 9,
    fontSize: 8,
    color: COLOR.inkFaint,
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
  dotColor,
}: {
  title: string;
  skills: string[];
  emptyHint: string;
  dotColor: string;
}) {
  return (
    <View style={styles.column}>
      <Text style={styles.sectionTitle}>{title}</Text>
      {skills.length === 0 ? (
        <Text style={styles.emptyHint}>{emptyHint}</Text>
      ) : (
        skills.map((skill, index) => (
          <View key={index} style={styles.skillRow} wrap={false}>
            <View style={[styles.skillDot, { backgroundColor: dotColor }]} />
            <Text style={styles.skillText}>{skill}</Text>
          </View>
        ))
      )}
    </View>
  );
}

function AnalysisPdfDoc({ data, locale, labels, dateText }: DocProps) {
  const title = data.jobTitle?.trim() || labels.defaultTitle;
  const metaParts = [data.cvFilename, dateText].filter(Boolean);
  const verdictStyle = VERDICT_STYLE[data.verdict];

  return (
    <Document title={title} author="Papyrus">
      <Page size="A4" style={styles.page}>
        <View style={styles.header} wrap={false}>
          <View style={styles.headerLeft}>
            <Text
              style={[
                styles.verdictPill,
                {
                  color: verdictStyle.color,
                  backgroundColor: verdictStyle.background,
                },
              ]}
            >
              {labels.verdict[data.verdict]}
            </Text>
            <Text style={styles.title}>{title}</Text>
            {metaParts.length > 0 ? (
              <Text style={styles.meta}>{metaParts.join("  ·  ")}</Text>
            ) : null}
          </View>

          <View style={styles.scoreCard}>
            <Text style={styles.scoreCaption}>{labels.fit}</Text>
            <View style={styles.scoreRow}>
              <Text style={styles.scoreValue}>{data.score}</Text>
              <Text style={styles.scoreOutOf}>/100</Text>
            </View>
          </View>
        </View>

        <View style={styles.divider} />

        <View style={styles.section}>
          <Text style={styles.sectionTitle}>{labels.summary}</Text>
          <Text style={styles.summary}>{data.summary[locale]}</Text>
        </View>

        <View style={[styles.section, styles.columns]}>
          <SkillColumn
            title={labels.matched}
            skills={data.matchedSkills[locale]}
            emptyHint={labels.matchedEmpty}
            dotColor={COLOR.positive}
          />
          <View style={styles.columnGap} />
          <SkillColumn
            title={labels.missing}
            skills={data.missingSkills[locale]}
            emptyHint={labels.missingEmpty}
            dotColor={COLOR.warning}
          />
        </View>

        {data.suggestions.length > 0 ? (
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{labels.improve}</Text>
            {data.suggestions.map((suggestion, index) => {
              const priorityStyle = PRIORITY_STYLE[suggestion.priority];
              return (
                <View key={index} style={styles.suggestion} wrap={false}>
                  <View style={styles.suggestionHead}>
                    <Text style={styles.suggestionTitle}>
                      {suggestion.title[locale]}
                    </Text>
                    <Text
                      style={[
                        styles.priorityTag,
                        {
                          color: priorityStyle.color,
                          backgroundColor: priorityStyle.background,
                        },
                      ]}
                    >
                      {labels.priority[suggestion.priority]}
                    </Text>
                  </View>
                  <Text style={styles.suggestionDetail}>
                    {suggestion.detail[locale]}
                  </Text>
                </View>
              );
            })}
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
