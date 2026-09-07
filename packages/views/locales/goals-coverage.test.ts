// @vitest-environment node
import { describe, expect, it } from "vitest";
import { RESOURCES } from "./index";

const LOCALES = ["en", "zh-Hans", "ja", "ko"] as const;

function flat(o: Record<string, unknown>, p = ""): [string, string][] {
  const out: [string, string][] = [];
  for (const [k, v] of Object.entries(o)) {
    if (v && typeof v === "object") out.push(...flat(v as Record<string, unknown>, `${p}${k}.`));
    else out.push([`${p}${k}`, String(v)]);
  }
  return out;
}

/**
 * Translation coverage for the goal layer.
 *
 * Scoped to this one namespace on purpose. The repo-wide version of the second
 * check reports 366 hits, and most are correct: "GitHub", "Plugins", "Lark",
 * "Composio" are proper nouns, and "English" in a language picker is the same
 * word whichever language you are reading it in. A guard that cries wolf 366
 * times teaches people to skip it.
 *
 * Here the set is small and every string in it is prose I can account for, so
 * an English sentence surviving into zh-Hans / ja / ko is always a mistake —
 * which is exactly the mistake the parity check next door cannot see, because
 * it compares key sets and never looks at a value.
 */
describe("goals namespace across every shipped locale", () => {
  it("has a non-empty string for every key in every language", () => {
    for (const locale of LOCALES) {
      const ns = (RESOURCES as Record<string, Record<string, unknown>>)[locale]?.goals;
      expect(ns, `${locale} ships no goals namespace`).toBeTruthy();
      for (const [key, value] of flat(ns as Record<string, unknown>)) {
        expect(value.trim(), `${locale}:${key} is empty`).not.toBe("");
      }
    }
  });

  it("does not leave English copy in the translated locales", () => {
    // The failure this catches is a key added to en and copy-pasted into the
    // others untouched — it passes the parity check, which only compares key
    // sets, and ships an English sentence to a Chinese reader.
    const en = new Map(flat((RESOURCES as never)["en"]["goals"]));
    for (const locale of ["zh-Hans", "ja", "ko"] as const) {
      const other = new Map(flat((RESOURCES as never)[locale]["goals"]));
      const identical = [...en.entries()].filter(
        ([key, value]) =>
          other.get(key) === value &&
          // Interpolation-only strings and bare tokens are legitimately the
          // same in every language.
          /[A-Za-z]{4}/.test(value.replace(/\{\{[^}]+\}\}/g, "")),
      );
      expect(identical.map(([k]) => k), `${locale} still carries English copy`).toEqual([]);
    }
  });
});
