import {
  Document,
  Font,
  Page,
  StyleSheet,
  Text,
  View,
  pdf,
} from "@react-pdf/renderer";

import type { TailoredCv } from "@/lib/types";

export interface CvLabels {
  summary: string;
  experience: string;
  skills: string;
  education: string;
}

// Inter (embedded) gives full Unicode coverage and a modern, professional look,
// where the built-in Helvetica is dated and limited to Latin-1.
Font.register({
  family: "Inter",
  fonts: [
    { src: "/fonts/Inter-Regular.ttf", fontWeight: 400 },
    { src: "/fonts/Inter-SemiBold.ttf", fontWeight: 600 },
    { src: "/fonts/Inter-Bold.ttf", fontWeight: 700 },
  ],
});
Font.registerHyphenationCallback((word) => [word]);

const COLOR = {
  ink: "#181a1f",
  inkMuted: "#4b4f57",
  inkFaint: "#8a8d94",
  line: "#e2e0db",
  accent: "#0072e6",
};

// All sizes scale by a single factor so the export can shrink to fit one page.
function makeStyles(s: number) {
  return StyleSheet.create({
    page: {
      paddingVertical: 36 * s,
      paddingHorizontal: 48 * s,
      fontFamily: "Inter",
      fontSize: 9.5 * s,
      color: COLOR.ink,
      lineHeight: 1.45,
    },
    name: { fontSize: 19 * s, fontWeight: 700, letterSpacing: 0.2 * s },
    headline: { fontSize: 10.5 * s, color: COLOR.accent, marginTop: 7 * s },
    contact: {
      fontSize: 8.5 * s,
      color: COLOR.inkFaint,
      marginTop: 6 * s,
      lineHeight: 1.4,
    },
    rule: {
      borderBottomWidth: 1,
      borderBottomColor: COLOR.line,
      marginTop: 14 * s,
      marginBottom: 13 * s,
    },
    sectionTitle: {
      fontSize: 8 * s,
      fontWeight: 700,
      color: COLOR.inkFaint,
      textTransform: "uppercase",
      letterSpacing: 1.2 * s,
      marginBottom: 6 * s,
    },
    section: { marginBottom: 12 * s },
    summary: { fontSize: 9.5 * s, color: COLOR.inkMuted, lineHeight: 1.5 },
    entry: { marginBottom: 9 * s },
    role: { fontSize: 10.5 * s, fontWeight: 600 },
    // Company and dates share one line in a single text flow — unambiguous for ATS
    // parsers, which can mis-order text split across flex columns.
    metaLine: { fontSize: 9 * s, color: COLOR.inkFaint, marginTop: 1 * s },
    bulletRow: { flexDirection: "row", marginTop: 3 * s, paddingRight: 4 * s },
    bulletDot: { width: 9 * s, fontSize: 9.5 * s, color: COLOR.inkFaint },
    bulletText: {
      flex: 1,
      fontSize: 9.5 * s,
      color: COLOR.inkMuted,
      lineHeight: 1.45,
    },
    skillLine: { fontSize: 9.5 * s, color: COLOR.inkMuted, marginBottom: 2.5 * s },
    skillLabel: { fontWeight: 600, color: COLOR.ink },
    eduLine: { fontSize: 9.5 * s, marginBottom: 3 * s },
    eduDegree: { fontWeight: 600 },
    eduMeta: { color: COLOR.inkMuted },
    extraItem: { fontSize: 9.5 * s, color: COLOR.inkMuted, marginBottom: 2.5 * s },
  });
}

type Styles = ReturnType<typeof makeStyles>;

function joinMeta(parts: (string | undefined)[]): string {
  return parts.filter((part) => part && part.trim()).join("  ·  ");
}

// Skill lines arrive as "Group: a, b, c"; bold the label up to the first colon.
function SkillLine({ value, styles }: { value: string; styles: Styles }) {
  const split = value.indexOf(":");
  if (split === -1) {
    return <Text style={styles.skillLine}>{value}</Text>;
  }
  return (
    <Text style={styles.skillLine}>
      <Text style={styles.skillLabel}>{value.slice(0, split + 1)}</Text>
      {value.slice(split + 1)}
    </Text>
  );
}

function CvDoc({
  cv,
  labels,
  scale,
}: {
  cv: TailoredCv;
  labels: CvLabels;
  scale: number;
}) {
  const styles = makeStyles(scale);
  return (
    <Document title={cv.fullName} author="Papyrus">
      <Page size="A4" style={styles.page}>
        <View wrap={false}>
          <Text style={styles.name}>{cv.fullName}</Text>
          {cv.headline ? <Text style={styles.headline}>{cv.headline}</Text> : null}
          {cv.contact ? <Text style={styles.contact}>{cv.contact}</Text> : null}
        </View>

        <View style={styles.rule} />

        {cv.summary ? (
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{labels.summary}</Text>
            <Text style={styles.summary}>{cv.summary}</Text>
          </View>
        ) : null}

        {cv.experience.length > 0 ? (
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{labels.experience}</Text>
            {cv.experience.map((item, index) => (
              <View key={index} style={styles.entry} wrap={false}>
                <Text style={styles.role}>{item.role}</Text>
                <Text style={styles.metaLine}>
                  {joinMeta([item.company, item.period])}
                </Text>
                {item.highlights.map((highlight, hi) => (
                  <View key={hi} style={styles.bulletRow}>
                    <Text style={styles.bulletDot}>•</Text>
                    <Text style={styles.bulletText}>{highlight}</Text>
                  </View>
                ))}
              </View>
            ))}
          </View>
        ) : null}

        {cv.skills.length > 0 ? (
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{labels.skills}</Text>
            {cv.skills.map((skill, index) => (
              <SkillLine key={index} value={skill} styles={styles} />
            ))}
          </View>
        ) : null}

        {cv.education.length > 0 ? (
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{labels.education}</Text>
            {cv.education.map((item, index) => {
              const meta = joinMeta([item.institution, item.period]);
              return (
                <Text key={index} style={styles.eduLine}>
                  <Text style={styles.eduDegree}>{item.degree}</Text>
                  {meta ? <Text style={styles.eduMeta}>{`  ·  ${meta}`}</Text> : null}
                </Text>
              );
            })}
          </View>
        ) : null}

        {cv.additional.map((sec, index) =>
          sec.items.length > 0 ? (
            <View key={index} style={styles.section} wrap={false}>
              <Text style={styles.sectionTitle}>{sec.title}</Text>
              {sec.items.map((entry, ei) => (
                <Text key={ei} style={styles.extraItem}>
                  {entry}
                </Text>
              ))}
            </View>
          ) : null,
        )}
      </Page>
    </Document>
  );
}

// Shrink progressively until the CV fits one page; never go below the last step.
const SCALES = [1, 0.95, 0.9, 0.85, 0.8];

export async function downloadTailoredCvPdf(
  cv: TailoredCv,
  labels: CvLabels,
  filename: string,
): Promise<void> {
  let blob: Blob | null = null;
  for (let i = 0; i < SCALES.length; i += 1) {
    const scale = SCALES[i] ?? 0.8;
    blob = await pdf(<CvDoc cv={cv} labels={labels} scale={scale} />).toBlob();
    if (i === SCALES.length - 1) break;
    if ((await countPages(blob)) <= 1) break;
  }
  if (blob) triggerDownload(blob, filename);
}

async function countPages(blob: Blob): Promise<number> {
  try {
    const { PDFDocument } = await import("pdf-lib");
    const doc = await PDFDocument.load(await blob.arrayBuffer());
    return doc.getPageCount();
  } catch {
    return 1;
  }
}

function triggerDownload(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
