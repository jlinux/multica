// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  daysBetween,
  defaultWindow,
  effectiveDate,
  monthTicks,
  parseCalendarDate,
  positionOf,
  shippedWithoutUseDays,
  type MilestoneLike,
} from "./timeline-window";

const day = (s: string) => {
  const at = parseCalendarDate(s);
  if (at === null) throw new Error(`fixture is not a calendar date: ${s}`);
  return at;
};

describe("parseCalendarDate", () => {
  it("reads a calendar day as UTC midnight, never as a local instant", () => {
    // The regression this pins: `new Date("2026-10-10")` is UTC midnight, and
    // rendering it in a zone behind UTC shows the 9th. Every milestone west of
    // Greenwich would sit one day left on the chart.
    const at = day("2026-10-10");
    expect(new Date(at).toISOString()).toBe("2026-10-10T00:00:00.000Z");
  });

  it("refuses days a month does not have instead of rolling them forward", () => {
    // Date.UTC(2026, 1, 30) silently becomes 2 March. A chart that moves a
    // date rather than dropping it is worse than one with a hole in it.
    expect(parseCalendarDate("2026-02-30")).toBeNull();
    expect(parseCalendarDate("2026-04-31")).toBeNull();
    expect(parseCalendarDate("2028-02-29")).not.toBeNull();
  });

  it("returns null for everything that is not a calendar day", () => {
    for (const bad of ["", null, undefined, "2026-10-10T00:00:00Z", "10/10/2026", "2026-13-01"]) {
      expect(parseCalendarDate(bad)).toBeNull();
    }
  });
});

describe("defaultWindow", () => {
  it("opens on whole months with more future than past", () => {
    // A roadmap is read forwards, but the recent past is what makes a missed
    // milestone visible at all.
    const w = defaultWindow(day("2026-10-14"));
    expect(new Date(w.start).toISOString().slice(0, 10)).toBe("2026-08-01");
    expect(new Date(w.end).toISOString().slice(0, 10)).toBe("2027-01-31");
  });

  it("crosses a year boundary without drifting", () => {
    const w = defaultWindow(day("2026-12-05"));
    expect(new Date(w.start).toISOString().slice(0, 10)).toBe("2026-10-01");
    expect(new Date(w.end).toISOString().slice(0, 10)).toBe("2027-03-31");
  });
});

describe("positionOf", () => {
  const w = { start: day("2026-08-01"), end: day("2027-01-31") };

  it("places the edges at 0 and 100", () => {
    expect(positionOf(w.start, w)).toBe(0);
    expect(positionOf(w.end, w)).toBe(100);
  });

  it("returns null outside the window rather than clamping to an edge", () => {
    // A marker pinned to the edge reads as "it happens here", which is a claim
    // the chart has no business making about a date it is not showing.
    expect(positionOf(day("2026-07-31"), w)).toBeNull();
    expect(positionOf(day("2027-02-01"), w)).toBeNull();
  });

  it("orders dates monotonically across the span", () => {
    const a = positionOf(day("2026-09-01"), w);
    const b = positionOf(day("2026-11-01"), w);
    expect(a).not.toBeNull();
    expect(b).not.toBeNull();
    expect(a as number).toBeLessThan(b as number);
  });
});

describe("monthTicks", () => {
  it("emits one tick per month the window touches", () => {
    const ticks = monthTicks({ start: day("2026-08-01"), end: day("2027-01-31") });
    expect(ticks.map((t) => new Date(t.at).toISOString().slice(0, 7))).toEqual([
      "2026-08",
      "2026-09",
      "2026-10",
      "2026-11",
      "2026-12",
      "2027-01",
    ]);
  });
});

describe("effectiveDate", () => {
  const base: MilestoneLike = {
    type: "launch",
    status: "planned",
    planned_date: "2026-10-10",
    actual_date: null,
    original_planned_date: "2026-10-03",
  };

  it("draws an achieved milestone where it landed, not where it was planned", () => {
    // The reschedule log exists precisely because the two are allowed to
    // differ; a chart that shows the plan would contradict the record.
    expect(effectiveDate({ ...base, status: "achieved", actual_date: "2026-10-08" })).toBe(
      day("2026-10-08"),
    );
  });

  it("falls back to the plan while a milestone is still open", () => {
    expect(effectiveDate(base)).toBe(day("2026-10-10"));
  });
});

describe("shippedWithoutUseDays", () => {
  const today = day("2026-10-14");
  const launched = (on: string): MilestoneLike => ({
    type: "launch",
    status: "achieved",
    planned_date: on,
    actual_date: on,
    original_planned_date: on,
  });

  it("counts the silence after a launch nothing has followed", () => {
    // The number this whole view exists to surface: every other tool draws a
    // launched goal as a finished bar.
    expect(shippedWithoutUseDays([launched("2026-08-08")], today)).toBe(67);
  });

  it("stops counting once a first use is confirmed", () => {
    const milestones: MilestoneLike[] = [
      launched("2026-08-08"),
      {
        type: "first_use",
        status: "achieved",
        planned_date: "2026-09-01",
        actual_date: "2026-09-03",
        original_planned_date: "2026-09-01",
      },
    ];
    expect(shippedWithoutUseDays(milestones, today)).toBeNull();
  });

  it("says nothing while an adoption milestone is still ahead of its date", () => {
    // A commitment in good standing is not a gap. Flagging it would train
    // people to read the warning as noise, and then to miss the real one.
    const milestones: MilestoneLike[] = [
      launched("2026-08-08"),
      {
        type: "first_use",
        status: "planned",
        planned_date: "2026-11-30",
        actual_date: null,
        original_planned_date: "2026-11-30",
      },
    ];
    expect(shippedWithoutUseDays(milestones, today)).toBeNull();
  });

  it("starts counting once that adoption date has passed unmet", () => {
    const milestones: MilestoneLike[] = [
      launched("2026-08-08"),
      {
        type: "first_use",
        status: "pending_accept",
        planned_date: "2026-09-30",
        actual_date: null,
        original_planned_date: "2026-09-30",
      },
    ];
    expect(shippedWithoutUseDays(milestones, today)).toBe(67);
  });

  it("does not count a goal that has not launched", () => {
    const planned: MilestoneLike = {
      type: "launch",
      status: "planned",
      planned_date: "2026-11-01",
      actual_date: null,
      original_planned_date: "2026-11-01",
    };
    expect(shippedWithoutUseDays([planned], today)).toBeNull();
    expect(shippedWithoutUseDays([], today)).toBeNull();
  });

  it("does not count the launch day itself", () => {
    expect(shippedWithoutUseDays([launched("2026-10-14")], today)).toBeNull();
  });
});

describe("daysBetween", () => {
  it("counts whole days and signs the direction", () => {
    expect(daysBetween(day("2026-10-03"), day("2026-10-10"))).toBe(7);
    expect(daysBetween(day("2026-10-10"), day("2026-10-03"))).toBe(-7);
  });

  it("is unaffected by a daylight-saving transition", () => {
    // Northern-hemisphere clocks move on 2026-10-25. Local-time arithmetic
    // would report 6 or 8 days across it; UTC-midnight days report 7.
    expect(daysBetween(day("2026-10-22"), day("2026-10-29"))).toBe(7);
  });
});
