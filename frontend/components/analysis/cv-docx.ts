import { Document, Packer, Paragraph, TextRun } from "docx";

import type { CvLabels } from "@/components/analysis/cv-pdf";
import type { TailoredCv } from "@/lib/types";

const ACCENT = "0072E6";
const FAINT = "8A8D94";

function sectionTitle(text: string): Paragraph {
  return new Paragraph({
    spacing: { before: 260, after: 80 },
    children: [
      new TextRun({
        text: text.toUpperCase(),
        bold: true,
        size: 18,
        color: FAINT,
        characterSpacing: 20,
      }),
    ],
  });
}

export async function downloadTailoredCvDocx(
  cv: TailoredCv,
  labels: CvLabels,
  filename: string,
): Promise<void> {
  const children: Paragraph[] = [
    new Paragraph({
      children: [new TextRun({ text: cv.fullName, bold: true, size: 36 })],
    }),
  ];

  if (cv.headline) {
    children.push(
      new Paragraph({
        children: [new TextRun({ text: cv.headline, size: 24, color: ACCENT })],
      }),
    );
  }
  if (cv.contact) {
    children.push(
      new Paragraph({
        spacing: { after: 160 },
        children: [new TextRun({ text: cv.contact, size: 18, color: FAINT })],
      }),
    );
  }

  if (cv.summary) {
    children.push(sectionTitle(labels.summary));
    children.push(
      new Paragraph({ children: [new TextRun({ text: cv.summary, size: 22 })] }),
    );
  }

  if (cv.experience.length > 0) {
    children.push(sectionTitle(labels.experience));
    for (const item of cv.experience) {
      children.push(
        new Paragraph({
          spacing: { before: 120 },
          children: [
            new TextRun({ text: item.role, bold: true, size: 22 }),
            item.period
              ? new TextRun({ text: `   ${item.period}`, size: 18, color: FAINT })
              : new TextRun(""),
          ],
        }),
      );
      if (item.company) {
        children.push(
          new Paragraph({
            children: [new TextRun({ text: item.company, size: 20, color: FAINT })],
          }),
        );
      }
      for (const highlight of item.highlights) {
        children.push(
          new Paragraph({
            bullet: { level: 0 },
            children: [new TextRun({ text: highlight, size: 21 })],
          }),
        );
      }
    }
  }

  if (cv.skills.length > 0) {
    children.push(sectionTitle(labels.skills));
    children.push(
      new Paragraph({
        children: [new TextRun({ text: cv.skills.join("  ·  "), size: 21 })],
      }),
    );
  }

  if (cv.education.length > 0) {
    children.push(sectionTitle(labels.education));
    for (const item of cv.education) {
      const tail = [item.institution, item.period].filter(Boolean).join(" · ");
      children.push(
        new Paragraph({
          spacing: { before: 40 },
          children: [
            new TextRun({ text: item.degree, size: 21 }),
            tail
              ? new TextRun({ text: `  —  ${tail}`, size: 20, color: FAINT })
              : new TextRun(""),
          ],
        }),
      );
    }
  }

  const doc = new Document({ sections: [{ children }] });
  const blob = await Packer.toBlob(doc);

  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
