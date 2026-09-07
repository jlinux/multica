"use client";

import { useQuery } from "@tanstack/react-query";
import { goalUsageOptions, type GoalUsage } from "@multica/core/goals";
import { useWorkspaceId } from "@multica/core/hooks";
import { useLocale, useT } from "../../i18n";
import { cn } from "@multica/ui/lib/utils";

/** The provider reports price in units of 1e-10 USD. */
const TICKS_PER_USD = 10_000_000_000;

/**
 * What a goal cost, rolled up through everything aligned beneath it.
 *
 * This is the one column no other product-management tool can fill, and the
 * reason is not cleverness — it is that none of them own the execution. The
 * chain from a token to a goal already exists here, so the number is a
 * by-product of running the plan rather than something anyone maintains, and
 * it stays true on the day everyone stops updating the board.
 *
 * With the adoption milestones supplying the numerator, a goal finally has
 * both halves of a return: what it cost, and whether anyone used what it
 * bought.
 *
 * Split by what ran, never by who ran it. Which CLI a goal's work went through
 * is an operational fact about tooling; attaching spend to a person is the
 * performance instrument this layer refuses to be.
 */
export function GoalCostCard({ goalId }: { goalId: string }) {
  const { t } = useT("goals");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const { data, isPending, isError } = useQuery(goalUsageOptions(wsId, goalId));

  if (isPending || isError || !data) return null;

  const uncosted = data.uncosted_input_tokens + data.uncosted_output_tokens;
  // Nothing has run and nothing is unpriced: there is no number here, and a
  // card reading $0.00 would state that the work was free rather than absent.
  if (data.agent_runs === 0 && uncosted === 0) return null;

  return (
    <div className="rounded-lg border border-surface-border bg-surface p-3">
      <div className="flex items-baseline gap-2">
        <h3 className="text-label font-semibold">{t(($) => $.cost.heading)}</h3>
        <p className="text-caption text-faint-foreground">{t(($) => $.cost.hint)}</p>
      </div>

      <div className="mt-2 flex flex-wrap items-end gap-x-6 gap-y-2">
        <Figure label={t(($) => $.cost.spend)} value={formatUsd(data.cost_usd_ticks, locale)} />
        <Figure label={t(($) => $.cost.runs)} value={String(data.agent_runs)} />
        <Figure label={t(($) => $.cost.issues)} value={String(data.issues)} />
      </div>

      {/* Reported, not folded in. A goal whose spend is unknown must not read
          as a goal that was cheap — that is the one interpretation of this
          number that would actively mislead. */}
      {uncosted > 0 && (
        <p
          className="mt-1.5 text-caption text-muted-foreground"
          title={t(($) => $.cost.uncosted_hint)}
        >
          {t(($) => $.cost.uncosted, { count: uncosted })}
        </p>
      )}

      {data.goals > 1 && (
        <p className="mt-1 text-micro text-faint-foreground">
          {t(($) => $.cost.rolled_up, { count: data.goals })}
        </p>
      )}

      {data.by_provider.length > 0 && (
        <div className="mt-3">
          <p className="mb-1.5 text-micro text-faint-foreground">
            {t(($) => $.cost.by_provider)}
          </p>
          <ProviderBar usage={data} locale={locale} />
        </div>
      )}
    </div>
  );
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-micro text-faint-foreground">{label}</div>
      <div className="text-title font-semibold tabular-nums">{value}</div>
    </div>
  );
}

const PROVIDER_TONES = ["bg-chart-1", "bg-chart-2", "bg-chart-3", "bg-chart-4", "bg-chart-5"];

function ProviderBar({ usage, locale }: { usage: GoalUsage; locale: string }) {
  // Ranked by spend, so the bar reads as "where the money went" left to right.
  // Rows the provider never priced fall to the end with a zero width and are
  // still listed below, because a tool that ran and was not priced is not a
  // tool that was not used.
  const total = usage.by_provider.reduce((sum, row) => sum + row.cost_usd_ticks, 0);

  return (
    <>
      {total > 0 && (
        <div className="flex h-1.5 gap-0.5 overflow-hidden rounded-full">
          {usage.by_provider.map((row, index) => (
            <span
              key={`${row.provider}-${row.model}`}
              className={cn("h-full rounded-full", PROVIDER_TONES[index % PROVIDER_TONES.length])}
              style={{ width: `${(row.cost_usd_ticks / total) * 100}%` }}
            />
          ))}
        </div>
      )}
      <ul className="mt-1.5 flex flex-wrap gap-x-4 gap-y-1">
        {usage.by_provider.map((row, index) => (
          <li
            key={`${row.provider}-${row.model}`}
            className="flex items-center gap-1.5 text-caption text-muted-foreground"
          >
            <span
              aria-hidden
              className={cn(
                "size-2 shrink-0 rounded-[2px]",
                PROVIDER_TONES[index % PROVIDER_TONES.length],
              )}
            />
            <span className="text-foreground">{row.model || row.provider}</span>
            <span className="tabular-nums">{formatUsd(row.cost_usd_ticks, locale)}</span>
          </li>
        ))}
      </ul>
    </>
  );
}

/**
 * Ticks to a readable amount.
 *
 * Cents below a hundred dollars, none above. A plan that cost four figures does
 * not read better for showing cents, and one that cost forty cents disappears
 * without them. The line sits at a hundred rather than ten because rounding
 * $42.50 to $43 quietly loses money at exactly the scale a small team notices.
 */
const CENTS_BELOW = 100;

function formatUsd(ticks: number, locale: string): string {
  const usd = ticks / TICKS_PER_USD;
  return new Intl.NumberFormat(locale, {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: usd < CENTS_BELOW ? 2 : 0,
  }).format(usd);
}
