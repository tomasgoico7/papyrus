import { describe, expect, it } from "vitest";

import { GatewayError, localizeGatewayError } from "@/lib/api/gateway";
import { getDictionary } from "@/lib/i18n/dictionaries";

const t = getDictionary("en");

describe("localizeGatewayError", () => {
  it("maps a known code to its localized message", () => {
    const error = new GatewayError("raw", "unreadable_cv", 400);
    expect(localizeGatewayError(error, t)).toBe(t.result.errors.unreadableCv);
  });

  it("falls back to the generic message for unknown codes", () => {
    const error = new GatewayError("raw", "totally_unknown", 500);
    expect(localizeGatewayError(error, t)).toBe(t.result.errorGeneric);
  });

  it("uses the session-expired copy for unauthorized", () => {
    const error = new GatewayError("raw", "unauthorized", 401);
    expect(localizeGatewayError(error, t)).toBe(t.result.sessionExpired);
  });

  it("substitutes the upload size limit", () => {
    const error = new GatewayError("raw", "payload_too_large", 413);
    expect(localizeGatewayError(error, t, 5)).toBe(
      t.result.errors.payloadTooLarge.replace("{mb}", "5"),
    );
  });
});
