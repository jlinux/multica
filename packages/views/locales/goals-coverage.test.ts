// @vitest-environment node
import { describe, expect, it } from "vitest";
import { RESOURCES } from "./index";

function flat(o: Record<string, unknown>, p = ""): [string, string][] {
  const out: [string, string][] = [];
  for (const [k, v] of Object.entries(o)) {
    if (v && typeof v === "object") {
      out.push(...flat(v as Record<string, unknown>, `${p}${k}.`));
    } else {
      out.push([`${p}${k}`, String(v)]);
    }
  }
  return out;
}

// These labels are established French words with the same spelling as English.
// Keep exceptions tied to individual keys so copied English prose still fails.
const IDENTICAL_TRANSLATIONS: Record<string, ReadonlySet<string>> = {
  fr: new Set([
    "detail.assignee_agent",
    "detail.cycle",
    "form.cycle",
    "form.description",
    "form.kind",
    "tier.direction",
    "view.panorama",
  ]),
};

/**
 * Parity checks key sets; this guard also catches empty translations and English
 * prose copied into the Goals namespace. Derive locales from the shipped bundle
 * so adding a language automatically extends this coverage.
 */
describe("goals namespace across every shipped locale", () => {
  for (const [locale, bundle] of Object.entries(RESOURCES)) {
    it(`${locale} has a non-empty string for every Goals key`, () => {
      const ns = bundle.goals;
      expect(ns, `${locale} ships no goals namespace`).toBeTruthy();
      for (const [key, value] of flat(ns as Record<string, unknown>)) {
        expect(value.trim(), `${locale}:${key} is empty`).not.toBe("");
      }
    });

    if (locale === "en") continue;

    it(`${locale} does not leave English copy in Goals`, () => {
      const en = new Map(flat(RESOURCES.en.goals as Record<string, unknown>));
      const other = new Map(flat(bundle.goals as Record<string, unknown>));
      const identical = [...en.entries()].filter(
        ([key, value]) =>
          other.get(key) === value &&
          !IDENTICAL_TRANSLATIONS[locale]?.has(key) &&
          // Interpolation-only strings and bare tokens may match legitimately.
          /[A-Za-z]{4}/.test(value.replace(/\{\{[^}]+\}\}/g, "")),
      );
      expect(identical.map(([key]) => key), `${locale} still carries English copy`).toEqual([]);
    });
  }
});
