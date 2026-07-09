<script lang="ts">
  import { analytics } from "../../stores/analytics.svelte.js";
  import { m } from "../../i18n/index.js";

  // Soft cap from the series-count ladder: past six skills the tail folds
  // into "Other" instead of generating more hues.
  const MAX_SERIES = 6;
  const OTHER_KEY = "__other__";
  const CHART_HEIGHT = 120;
  const MAX_X_LABELS = 14;

  interface Segment {
    key: string;
    label: string;
    count: number;
    colorIndex: number | null;
  }

  const trendEntries = $derived(analytics.skills?.trend ?? []);

  const skillTotals = $derived.by(() => {
    const totals = new Map<string, number>();
    for (const entry of trendEntries) {
      for (const [skill, count] of Object.entries(entry.by_skill)) {
        totals.set(skill, (totals.get(skill) ?? 0) + count);
      }
    }
    return totals;
  });

  // Fixed series order by overall volume; color follows the skill for the
  // whole render, so legend toggles never repaint the survivors.
  const topSkills = $derived.by(() => {
    return [...skillTotals.entries()]
      .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
      .slice(0, MAX_SERIES)
      .map(([skill]) => skill);
  });

  const otherTotal = $derived.by(() => {
    let total = 0;
    const top = new Set(topSkills);
    for (const [skill, count] of skillTotals) {
      if (!top.has(skill)) total += count;
    }
    return total;
  });

  interface LegendItem {
    key: string;
    label: string;
    total: number;
    colorIndex: number | null;
  }

  const legendItems = $derived.by(() => {
    const items: LegendItem[] = topSkills.map((skill, i) => ({
      key: skill,
      label: skill,
      total: skillTotals.get(skill) ?? 0,
      colorIndex: i,
    }));
    if (otherTotal > 0) {
      items.push({
        key: OTHER_KEY,
        label: m.analytics_skill_trend_other(),
        total: otherTotal,
        colorIndex: null,
      });
    }
    return items;
  });

  let hiddenKeys = $state<string[]>([]);

  function toggleSeries(key: string) {
    hiddenKeys = hiddenKeys.includes(key)
      ? hiddenKeys.filter((k) => k !== key)
      : [...hiddenKeys, key];
  }

  // Segments for one week, bottom-up in fixed series order with the
  // "Other" fold on top; hidden series are dropped before stacking.
  function segmentsFor(bySkill: Record<string, number>): Segment[] {
    const segments: Segment[] = [];
    let other = 0;
    const top = new Set(topSkills);
    for (const [skill, count] of Object.entries(bySkill)) {
      if (!top.has(skill)) other += count;
    }
    topSkills.forEach((skill, i) => {
      const count = bySkill[skill] ?? 0;
      if (count > 0 && !hiddenKeys.includes(skill)) {
        segments.push({
          key: skill,
          label: skill,
          count,
          colorIndex: i,
        });
      }
    });
    if (other > 0 && !hiddenKeys.includes(OTHER_KEY)) {
      segments.push({
        key: OTHER_KEY,
        label: m.analytics_skill_trend_other(),
        count: other,
        colorIndex: null,
      });
    }
    return segments;
  }

  const weeks = $derived(
    trendEntries.map((entry) => ({
      date: entry.date,
      segments: segmentsFor(entry.by_skill),
    })),
  );

  const maxTotal = $derived.by(() => {
    let max = 1;
    for (const week of weeks) {
      let total = 0;
      for (const segment of week.segments) {
        total += segment.count;
      }
      if (total > max) max = total;
    }
    return max;
  });

  function segmentHeight(count: number): number {
    return Math.max(Math.round((count / maxTotal) * CHART_HEIGHT), 2);
  }

  function seriesColor(colorIndex: number | null): string {
    if (colorIndex === null) return "var(--chart-series-other)";
    return `var(--chart-series-${colorIndex + 1})`;
  }

  const labelStep = $derived(
    Math.max(Math.ceil(trendEntries.length / MAX_X_LABELS), 1),
  );

  function weekLabel(date: string, index: number): string {
    if (index % labelStep !== 0) return "";
    if (date.length < 10) return date;
    return date.slice(5);
  }

  let tooltip = $state<{
    x: number;
    y: number;
    text: string;
  } | null>(null);

  function handleSegmentHover(
    e: MouseEvent,
    date: string,
    segment: Segment,
  ) {
    const rect = (
      e.currentTarget as HTMLElement
    ).getBoundingClientRect();
    tooltip = {
      x: rect.left + rect.width / 2,
      y: rect.top - 4,
      text: m.analytics_skill_trend_segment_tooltip({
        date,
        skill: segment.label,
        countLabel: segment.count.toLocaleString(),
      }),
    };
  }

  function handleLeave() {
    tooltip = null;
  }
</script>

<div class="trend-container">
  <div class="trend-header">
    <h3 class="chart-title">{m.analytics_skill_trend_title()}</h3>
    <span class="granularity">{m.analytics_weekly_trend()}</span>
  </div>

  {#if analytics.errors.skills}
    <div class="error">
      {analytics.errors.skills}
      <button
        class="retry-btn"
        onclick={() => analytics.fetchSkills()}
      >
        {m.shared_retry()}
      </button>
    </div>
  {:else if analytics.loading.skills && trendEntries.length === 0}
    <div class="empty">{m.analytics_skill_trend_loading()}</div>
  {:else if trendEntries.length > 0 && legendItems.length > 0}
    <div
      class="legend"
      role="group"
      aria-label={m.analytics_skill_trend_legend()}
    >
      {#each legendItems as item (item.key)}
        <button
          class="legend-chip"
          class:hidden-series={hiddenKeys.includes(item.key)}
          aria-pressed={!hiddenKeys.includes(item.key)}
          onclick={() => toggleSeries(item.key)}
        >
          <span
            class="legend-dot"
            style="background: {seriesColor(item.colorIndex)}"
          ></span>
          <span class="legend-name">{item.label}</span>
          <span class="legend-count">
            {item.total.toLocaleString()}
          </span>
        </button>
      {/each}
    </div>

    <div class="chart" style="height: {CHART_HEIGHT + 16}px">
      {#each weeks as week, weekIndex (week.date)}
        <div class="week-column">
          <div class="stack" style="height: {CHART_HEIGHT}px">
            {#each week.segments as segment (segment.key)}
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div
                class="segment"
                style="height: {segmentHeight(segment.count)}px;
                  background: {seriesColor(segment.colorIndex)}"
                onmouseenter={(e) =>
                  handleSegmentHover(e, week.date, segment)}
                onmouseleave={handleLeave}
              ></div>
            {/each}
          </div>
          <span class="week-label">
            {weekLabel(week.date, weekIndex)}
          </span>
        </div>
      {/each}
    </div>

    {#if tooltip}
      <div
        class="tooltip"
        style="left: {tooltip.x}px; top: {tooltip.y}px;"
      >
        {tooltip.text}
      </div>
    {/if}
  {:else}
    <div class="empty">{m.analytics_skill_trend_empty()}</div>
  {/if}
</div>

<style>
  /* Series colors come from the --chart-series-* app tokens in app.css,
     which carry their own light/dark steps. */
  .trend-container {
    position: relative;
    flex: 1;
  }

  .trend-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
    gap: 12px;
  }

  .chart-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .granularity {
    font-size: 10px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .legend {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px;
    margin-bottom: 10px;
  }

  .legend-chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    max-width: 220px;
    padding: 2px 7px;
    border: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
    background: var(--bg-inset);
    color: var(--text-secondary);
    font-size: 10px;
    cursor: pointer;
    transition: opacity 0.1s, background 0.1s;
  }

  .legend-chip:hover {
    background: var(--bg-surface-hover);
  }

  .legend-chip.hidden-series {
    opacity: 0.45;
  }

  .legend-chip.hidden-series .legend-dot {
    background: var(--text-muted) !important;
  }

  .legend-dot {
    flex-shrink: 0;
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }

  .legend-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .legend-count {
    font-family: var(--font-mono);
    color: var(--text-muted);
  }

  .chart {
    display: flex;
    align-items: flex-end;
    gap: var(--space-2);
  }

  .week-column {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: flex-end;
    height: 100%;
  }

  /* Bottom-up stacking; the 2px surface gap is the segment separator. */
  .stack {
    width: 100%;
    max-width: 24px;
    display: flex;
    flex-direction: column-reverse;
    justify-content: flex-start;
    gap: 2px;
  }

  .segment {
    width: 100%;
    flex-shrink: 0;
  }

  /* Rounded data-end on the top of the stack, square at the baseline. */
  .segment:last-child {
    border-radius: 4px 4px 0 0;
  }

  .segment:hover {
    opacity: 0.8;
  }

  .week-label {
    font-size: 8px;
    color: var(--text-muted);
    margin-top: 4px;
    white-space: nowrap;
    min-height: 10px;
  }

  .tooltip {
    position: fixed;
    transform: translateX(-50%) translateY(-100%);
    padding: 4px 8px;
    background: var(--text-primary);
    color: var(--bg-primary);
    font-size: 10px;
    border-radius: var(--radius-sm);
    white-space: nowrap;
    pointer-events: none;
    z-index: var(--z-tooltip);
  }

  .empty {
    color: var(--text-muted);
    font-size: 12px;
    padding: 24px;
    text-align: center;
  }

  .error {
    color: var(--accent-red);
    font-size: 12px;
    padding: 12px;
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .retry-btn {
    padding: 2px 8px;
    border: 1px solid currentColor;
    border-radius: var(--radius-sm);
    font-size: 11px;
    color: inherit;
    cursor: pointer;
  }
</style>
