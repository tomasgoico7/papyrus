import { describe, expect, it } from "vitest";

import { cvFileName, formatDate, slugify } from "@/lib/utils";

describe("formatDate", () => {
  it("formats English dates as 'Mon D, YYYY'", () => {
    expect(formatDate("2026-06-11T12:00:00Z", "en")).toBe("Jun 11, 2026");
  });

  it("formats Spanish dates as 'D mon YYYY'", () => {
    expect(formatDate("2026-06-11T12:00:00Z", "es")).toBe("11 jun 2026");
  });

  it("defaults to English", () => {
    expect(formatDate("2026-12-01T00:00:00Z")).toBe("Dec 1, 2026");
  });

  // The hydration fix depends on this being timezone-independent: the same
  // instant must yield the same calendar day on the server and the client.
  it("uses UTC regardless of the host timezone", () => {
    expect(formatDate("2026-01-31T23:30:00Z", "en")).toBe("Jan 31, 2026");
  });
});

describe("slugify", () => {
  it("strips accents", () => {
    expect(slugify("Análisis Señor")).toBe("analisis-senor");
  });

  it("collapses runs of non-alphanumerics into single hyphens", () => {
    expect(slugify("Senior  Backend / Node.js")).toBe("senior-backend-node-js");
  });

  it("trims leading and trailing hyphens", () => {
    expect(slugify("  hola!  ")).toBe("hola");
  });

  it("falls back to 'analysis' when nothing remains", () => {
    expect(slugify("¡!!")).toBe("analysis");
  });

  it("caps the length at 50 characters", () => {
    expect(slugify("a".repeat(80))).toHaveLength(50);
  });
});

describe("cvFileName", () => {
  it("builds Name_Surname_CV from a full name", () => {
    expect(cvFileName("Tomás Goicoechea")).toBe("Tomas_Goicoechea_CV");
  });

  it("preserves the source casing", () => {
    expect(cvFileName("TOMÁS GOICOECHEA")).toBe("TOMAS_GOICOECHEA_CV");
  });

  it("collapses punctuation and extra spaces", () => {
    expect(cvFileName("  Anne-Marie   O'Neil ")).toBe("Anne_Marie_O_Neil_CV");
  });

  it("falls back to CV when the name has no letters", () => {
    expect(cvFileName("—")).toBe("CV");
  });
});
