import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { testUser } from "@/test-utils";
import { canAdminister, meQueryKey, resetProfileData } from "./useAuth";

describe("canAdminister", () => {
  const kid = { id: "0190a8b0-0000-7000-8000-0000000000k1", name: "Mia", color: "#fff", isKid: true, hasPin: false };
  it("is true only for an administrator outside a kid profile", () => {
    expect(canAdminister(testUser({ isAdmin: true }))).toBe(true);
    expect(canAdminister(testUser({ isAdmin: false }))).toBe(false);
    expect(canAdminister(testUser({ isAdmin: true, currentProfile: kid } as never))).toBe(false);
    expect(canAdminister(null)).toBe(false);
    expect(canAdminister(undefined)).toBe(false);
  });
});

describe("resetProfileData", () => {
  it("drops the previous profile's rows but keeps the account and server", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    qc.setQueryData(meQueryKey, testUser());
    qc.setQueryData(["system", "info"], { name: "x" });
    qc.setQueryData(["profiles"], []);
    qc.setQueryData(["continue"], [{ id: "a" }]);
    qc.setQueryData(["preferences"], { subtitleMode: "always" });
    await resetProfileData(qc);
    expect(qc.getQueryData(["continue"])).toBeUndefined();
    expect(qc.getQueryData(["preferences"])).toBeUndefined();
    expect(qc.getQueryData(["system", "info"])).toEqual({ name: "x" });
    expect(qc.getQueryData(["profiles"])).toEqual([]);
    expect(qc.getQueryState(meQueryKey)?.isInvalidated).toBe(true);
  });
});
