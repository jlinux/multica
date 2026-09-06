/**
 * Date arithmetic for the goal roadmap.
 *
 * Everything here works on calendar days, never on instants. A milestone date
 * is "2026-10-10" with no time and no zone: it is the same day for a reader in
 * Auckland and one in Los Angeles. Parsing those strings with `new Date(s)`
 * would anchor them to UTC midnight and then render them in the reader's zone,
 * which moves half the world's milestones one day to the left. So the strings
 * are parsed by hand and every value here is a UTC-midnight timestamp.
 *
 * Pure on purpose: `today` is always a parameter, so the tests can pin the
 * boundaries the eye cannot check on a chart.
 */

const CALENDAR_DATE = /^(\d{4})-(\d{2})-(\d{2})$/;

export const MS_PER_DAY = 86_400_000;

/** Parses "YYYY-MM-DD" to a UTC-midnight timestamp, or null if it is not one. */
export function parseCalendarDate(value: string | null | undefined): number | null {
  if (!value) return null;
  const match = CALENDAR_DATE.exec(value);
  if (!match) return null;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  if (month < 1 || month > 12 || day < 1 || day > 31) return null;
  const at = Date.UTC(year, month - 1, day);
  // Rejects the days a month does not have: Date.UTC rolls 2026-02-30 forward
  // to March, and a chart that silently moves a date is worse than one that
  // drops it.
  const back = new Date(at);
  if (back.getUTCMonth() !== month - 1 || back.getUTCDate() !== day) return null;
  return at;
}

export interface TimelineWindow {
  /** UTC-midnight timestamp of the first day shown. */
  start: number;
  /** UTC-midnight timestamp of the last day shown. */
  end: number;
}

/**
 * The window the roadmap opens on: whole months, a little history and more
 * future.
 *
 * The asymmetry is the point. A roadmap is read forwards — "what lands next" —
 * but the past two months are what make an unmet milestone visible at all, and
 * a window that starts today hides every slip that already happened.
 */
export function defaultWindow(today: number, monthsBefore = 2, monthsAfter = 3): TimelineWindow {
  const at = new Date(today);
  const start = Date.UTC(at.getUTCFullYear(), at.getUTCMonth() - monthsBefore, 1);
  // Day 0 of the month after the last one shown is that month's final day.
  const end = Date.UTC(at.getUTCFullYear(), at.getUTCMonth() + monthsAfter + 1, 0);
  return { start, end };
}

/**
 * Where a date sits in the window, as a percentage of its width.
 *
 * Returns null outside the window rather than a clamped value: a marker pinned
 * to the edge reads as "it happens here", which is a claim the chart has no
 * business making about a date it is not showing.
 */
export function positionOf(date: number, window: TimelineWindow): number | null {
  if (date < window.start || date > window.end) return null;
  const span = window.end - window.start;
  if (span <= 0) return null;
  return ((date - window.start) / span) * 100;
}

/** Whole days from a to b, negative when b is earlier. */
export function daysBetween(a: number, b: number): number {
  return Math.round((b - a) / MS_PER_DAY);
}

/** The first day of every month the window touches, for the scale. */
export function monthTicks(window: TimelineWindow): { at: number; position: number }[] {
  const ticks: { at: number; position: number }[] = [];
  const first = new Date(window.start);
  let year = first.getUTCFullYear();
  let month = first.getUTCMonth();
  // Bounded by the window itself; the guard is against a caller passing an
  // inverted or absurd range rather than against normal input.
  for (let guard = 0; guard < 240; guard += 1) {
    const at = Date.UTC(year, month, 1);
    if (at > window.end) break;
    const position = positionOf(at, window);
    if (position !== null) ticks.push({ at, position });
    month += 1;
    if (month > 11) {
      month = 0;
      year += 1;
    }
  }
  return ticks;
}

export interface MilestoneLike {
  type: string;
  status: string;
  planned_date: string | null;
  actual_date: string | null;
  original_planned_date: string | null;
}

/**
 * The date a milestone occupies on the chart: where it actually landed if it
 * has, otherwise where it is planned.
 *
 * An achieved milestone drawn on its planned date would put the chart at odds
 * with the record, and the whole reason the reschedule log exists is that the
 * two are allowed to differ.
 */
export function effectiveDate(milestone: MilestoneLike): number | null {
  return (
    parseCalendarDate(milestone.actual_date) ?? parseCalendarDate(milestone.planned_date)
  );
}

/**
 * How long a goal has been shipped without anyone using it, or null when that
 * is not the situation.
 *
 * This is the number the roadmap exists to surface. Every other tool draws a
 * launched goal as a finished bar; here a launch with nothing after it grows a
 * visible, dated gap that keeps widening until someone either confirms a first
 * use or admits the feature is not being adopted.
 *
 * Null when the goal has not launched, when adoption has already been reached,
 * or when an adoption milestone exists and is still ahead of its date — a plan
 * that has not come due yet is not a failure.
 */
export function shippedWithoutUseDays(
  milestones: MilestoneLike[],
  today: number,
): number | null {
  const launch = milestones.find((m) => m.type === "launch" && m.status === "achieved");
  if (!launch) return null;
  const launchedAt = parseCalendarDate(launch.actual_date);
  if (launchedAt === null) return null;

  const adoption = milestones.filter((m) => m.type === "first_use");
  if (adoption.some((m) => m.status === "achieved")) return null;
  // An adoption milestone that is planned and not yet due is a commitment in
  // good standing, not a gap. Only once its own date has passed does the
  // silence start counting.
  if (adoption.some((m) => {
    const due = parseCalendarDate(m.planned_date);
    return due !== null && due >= today && m.status !== "cancelled";
  })) {
    return null;
  }

  const days = daysBetween(launchedAt, today);
  return days > 0 ? days : null;
}
