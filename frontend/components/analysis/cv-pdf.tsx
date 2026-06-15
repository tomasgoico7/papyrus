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

Font.registerHyphenationCallback((word) => [word]);

const COLOR = {
  ink: "#181a1f",
  inkMuted: "#4b4f57",
  inkFaint: "#8a8d94",
  line: "#e2e0db",
  accent: "#0072e6",
};

const styles = StyleSheet.create({
  page: {
    paddingVertical: 44,
    paddingHorizontal: 52,
    fontFamily: "Helvetica",
    fontSize: 10,
    color: COLOR.ink,
    lineHeight: 1.5,
  },
  name: { fontSize: 22, fontFamily: "Helvetica-Bold" },
  headline: { fontSize: 11, color: COLOR.accent, marginTop: 2 },
  contact: { fontSize: 9, color: COLOR.inkFaint, marginTop: 4 },
  rule: {
    borderBottomWidth: 1,
    borderBottomColor: COLOR.line,
    marginTop: 16,
    marginBottom: 16,
  },
  sectionTitle: {
    fontSize: 8.5,
    fontFamily: "Helvetica-Bold",
    color: COLOR.inkFaint,
    textTransform: "uppercase",
    letterSpacing: 1.2,
    marginBottom: 8,
  },
  section: { marginBottom: 18 },
  summary: { fontSize: 10.5, lineHeight: 1.6 },
  entry: { marginBottom: 12 },
  entryHead: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "flex-start",
  },
  role: { fontSize: 11, fontFamily: "Helvetica-Bold", flex: 1, paddingRight: 12 },
  period: { fontSize: 9, color: COLOR.inkFaint },
  company: { fontSize: 10, color: COLOR.inkMuted, marginTop: 1 },
  bulletRow: { flexDirection: "row", marginTop: 4, paddingRight: 6 },
  bulletDot: { width: 10, fontSize: 10, color: COLOR.inkFaint },
  bulletText: { flex: 1, fontSize: 9.5, color: COLOR.inkMuted, lineHeight: 1.5 },
  skills: { fontSize: 10, color: COLOR.inkMuted, lineHeight: 1.6 },
  eduRow: {
    flexDirection: "row",
    justifyContent: "space-between",
    marginBottom: 4,
  },
  degree: { fontSize: 10, flex: 1, paddingRight: 12 },
  institution: { color: COLOR.inkMuted },
});

function CvDoc({ cv, labels }: { cv: TailoredCv; labels: CvLabels }) {
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
                <View style={styles.entryHead}>
                  <Text style={styles.role}>{item.role}</Text>
                  <Text style={styles.period}>{item.period}</Text>
                </View>
                <Text style={styles.company}>{item.company}</Text>
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
            <Text style={styles.skills}>{cv.skills.join("  ·  ")}</Text>
          </View>
        ) : null}

        {cv.education.length > 0 ? (
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{labels.education}</Text>
            {cv.education.map((item, index) => (
              <View key={index} style={styles.eduRow}>
                <Text style={styles.degree}>
                  {item.degree}
                  {item.institution ? (
                    <Text style={styles.institution}> — {item.institution}</Text>
                  ) : null}
                </Text>
                <Text style={styles.period}>{item.period}</Text>
              </View>
            ))}
          </View>
        ) : null}
      </Page>
    </Document>
  );
}

export async function downloadTailoredCvPdf(
  cv: TailoredCv,
  labels: CvLabels,
  filename: string,
): Promise<void> {
  const blob = await pdf(<CvDoc cv={cv} labels={labels} />).toBlob();
  triggerDownload(blob, filename);
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
