// @vitest-environment node
import { describe, expect, it } from "vitest";
import { validateWecomGuestGroup } from "./guest-access";

describe("manual WeCom guest group validation", () => {
  it.each([
    ["", [], "empty"], ["  ", [], "empty"], ["group-a", ["group-a"], "duplicate"],
    ["*", [], "wildcard"], ["group-*", [], "wildcard"], ["group-?", [], "wildcard"], ["a".repeat(257), [], "too_long"],
    ["群".repeat(86), [], "too_long"], ["new", Array.from({ length: 100 }, (_, n) => String(n)), "too_many"],
    ["real-group", [], null], ["a".repeat(256), [], null],
  ] as const)("validates %s", (id, groups, result) => {
    expect(validateWecomGuestGroup(id, groups)).toBe(result);
  });
});
