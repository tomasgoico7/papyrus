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
    paddingVertical: 34,
    paddingHorizontal: 46,
    fontFamily: "Helvetica",
    fontSize: 9.5,
    color: COLOR.ink,
    lineHeight: 1.4,
  },
  name: { fontSize: 18, fontFamily: "Helvetica-Bold" },
  headline: { fontSize: 10.5, color: COLOR.accent, marginTop: 2 },
  contact: { fontSize: 8.5, color: COLOR.inkFaint, marginTop: 3 },
  rule: {
    borderBottomWidth: 1,
    borderBottomColor: COLOR.line,
    marginTop: 11,
    marginBottom: 11,
  },
  sectionTitle: {
    fontSize: 8,
    fontFamily: "Helvetica-Bold",
    color: COLOR.inkFaint,
    textTransform: "uppercase",
    letterSpacing: 1.2,
    marginBottom: 5,
  },
  section: { marginBottom: 11 },
  summary: { fontSize: 10, lineHeight: 1.45 },
  entry: { marginBottom: 8 },
  entryHead: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "flex-start",
  },
  role: { fontSize: 10.5, fontFamily: "Helvetica-Bold", flex: 1, paddingRight: 12 },
  period: { fontSize: 8.5, color: COLOR.inkFaint },
  company: { fontSize: 9.5, color: COLOR.inkMuted, marginTop: 1 },
  bulletRow: { flexDirection: "row", marginTop: 2.5, paddingRight: 4 },
  bulletDot: { width: 9, fontSize: 9.5, color: COLOR.inkFaint },
  bulletText: { flex: 1, fontSize: 9.5, color: COLOR.inkMuted, lineHeight: 1.4 },
  skillLine: { fontSize: 9.5, color: COLOR.inkMuted, marginBottom: 2.5 },
  skillLabel: { fontFamily: "Helvetica-Bold", color: COLOR.ink },
  eduRow: {
    flexDirection: "row",
    justifyContent: "space-between",
    marginBottom: 3,
  },
  degree: { fontSize: 9.5, flex: 1, paddingRight: 12 },
  institution: { color: COLOR.inkMuted },
});

// Skill lines arrive as "Group: a, b, c"; bold the label up to the first colon.
function SkillLine({ value }: { value: string }) {
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
            {cv.skills.map((skill, index) => (
              <SkillLine key={index} value={skill} />
            ))}
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

        {cv.additional.map((sec, index) =>
          sec.items.length > 0 ? (
            <View key={index} style={styles.section}>
              <Text style={styles.sectionTitle}>{sec.title}</Text>
              <Text style={styles.skillLine}>{sec.items.join("  ·  ")}</Text>
            </View>
          ) : null,
        )}
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
