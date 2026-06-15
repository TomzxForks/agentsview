<script lang="ts">
  import { analytics } from "../../stores/analytics.svelte.js";
  import { router } from "../../stores/router.svelte.js";
  import type {
    TPSResponse,
    TPSTurn,
    TPSPercentiles,
  } from "../../api/types.js";

  type Period =
    | "session"
    | "hour"
    | "day"
    | "dateHour"
    | "dayOfWeek"
    | "dayOfMonth"
    | "month";

  let activePeriod = $state<Period>("day");

  const PERIODS: { value: Period; label: string }[] = [
    { value: "session", label: "Session" },
    { value: "hour", label: "Hour" },
    { value: "day", label: "Date" },
    { value: "dateHour", label: "Date+Hr" },
    { value: "dayOfWeek", label: "DOW" },
    { value: "dayOfMonth", label: "DOM" },
    { value: "month", label: "Month" },
  ];

  const tps = $derived(analytics.tps);

  function fmt(v: number, decimals = 1): string {
    if (v <= 0) return "-";
    return v.toFixed(decimals);
  }

  function fmtInt(v: number): string {
    return v.toLocaleString();
  }

  function fmtDuration(sec: number): string {
    if (sec <= 0) return "-";
    if (sec < 60) return `${sec.toFixed(1)}s`;
    const m = Math.floor(sec / 60);
    const s = Math.round(sec % 60);
    return s > 0 ? `${m}m ${s}s` : `${m}m`;
  }

  function fmtPct(p: TPSPercentiles): string {
    return `${fmt(p.p50)} / ${fmt(p.p75)} / ${fmt(p.p90)} / ${fmt(p.p95)} / ${fmt(p.p_max)}`;
  }

  // --- Client-side aggregation (ported from tps-viewer) ---

  type AggGroup = {
    label: string;
    avgTps: number;
    avgItps: number;
    avgOtps: number;
    count: number;
    sortKey: number | string;
  };

  function aggregateByPeriod(
    turns: TPSTurn[],
    period: Period,
  ): AggGroup[] {
    const map = new Map<
      string,
      {
        tps: number;
        itps: number;
        otps: number;
        count: number;
        sortKey: number | string;
      }
    >();

    for (const t of turns) {
      const d = new Date(t.timestamp);
      let key: string;
      let sortKey: number | string;

      switch (period) {
        case "session":
          key = t.session_id.slice(0, 8);
          sortKey = key;
          break;
        case "hour":
          key = String(d.getHours());
          sortKey = d.getHours();
          break;
        case "day":
          key = dateStr(d);
          sortKey = d.getTime();
          break;
        case "dateHour":
          key = `${dateStr(d)} ${String(d.getHours()).padStart(2, "0")}:00`;
          sortKey = d.getTime();
          break;
        case "dayOfWeek":
          key = d.toLocaleDateString("en-US", { weekday: "short" });
          sortKey = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"].indexOf(
            key,
          );
          break;
        case "dayOfMonth":
          key = String(d.getDate());
          sortKey = d.getDate();
          break;
        case "month":
          key = d.toISOString().slice(0, 7);
          sortKey = d.getFullYear() * 12 + d.getMonth();
          break;
      }

      const g = map.get(key);
      if (g) {
        g.tps += t.tps;
        g.itps += t.itps;
        g.otps += t.otps;
        g.count++;
      } else {
        map.set(key, {
          tps: t.tps,
          itps: t.itps,
          otps: t.otps,
          count: 1,
          sortKey,
        });
      }
    }

    const out: AggGroup[] = [];
    for (const [label, g] of map) {
      out.push({
        label,
        avgTps: g.tps / g.count,
        avgItps: g.itps / g.count,
        avgOtps: g.otps / g.count,
        count: g.count,
        sortKey: g.sortKey,
      });
    }

    out.sort((a, b) => {
      if (period === "dayOfWeek") {
        return (
          (a.sortKey as number) - (b.sortKey as number)
        );
      }
      if (
        period === "hour" ||
        period === "dayOfMonth"
      ) {
        return (
          (a.sortKey as number) - (b.sortKey as number)
        );
      }
      if (
        period === "day" ||
        period === "month" ||
        period === "dateHour"
      ) {
        return (
          (a.sortKey as number) - (b.sortKey as number)
        );
      }
      return String(a.sortKey).localeCompare(String(b.sortKey));
    });

    return out;
  }

  function dateStr(d: Date): string {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    return `${y}-${m}-${day}`;
  }

  // --- Chart computation ---

  const BAR_HEIGHT = 140;
  const LABEL_HEIGHT = 20;
  const TOP_PAD = 16;
  const CHART_H = BAR_HEIGHT + LABEL_HEIGHT + TOP_PAD + 4;
  const BAR_GAP = 2;

  let containerEl = $state<HTMLDivElement | null>(null);
  let containerWidth = $state(600);

  $effect(() => {
    if (!containerEl) return;
    const obs = new ResizeObserver((entries) => {
      for (const entry of entries) {
        containerWidth = entry.contentRect.width;
      }
    });
    obs.observe(containerEl);
    return () => obs.disconnect();
  });

  const chart = $derived.by(() => {
    if (!tps || tps.turns.length === 0) {
      return {
        groups: [] as (AggGroup & {
          x: number;
          width: number;
          tpsH: number;
          itpsH: number;
          otpsH: number;
        })[],
        maxVal: 0,
        svgW: 600,
        labelStep: 1,
      };
    }

    const groups = aggregateByPeriod(tps.turns, activePeriod);
    if (groups.length === 0)
      return {
        groups: [],
        maxVal: 0,
        svgW: containerWidth,
        labelStep: 1,
      };

    const maxVal = Math.max(
      ...groups.map((g) => Math.max(g.avgTps, g.avgItps, g.avgOtps)),
      1,
    );

    const availW = containerWidth - 4;
    const groupW = Math.max(
      24,
      Math.floor(availW / groups.length),
    );
    const barW = Math.max(2, Math.floor((groupW - BAR_GAP * 4) / 3));

    const computed = groups.map((g, i) => ({
      ...g,
      x: i * groupW + 2,
      width: barW,
      tpsH: (g.avgTps / maxVal) * BAR_HEIGHT,
      itpsH: (g.avgItps / maxVal) * BAR_HEIGHT,
      otpsH: (g.avgOtps / maxVal) * BAR_HEIGHT,
      groupW,
    }));

    const svgW =
      computed.length > 0
        ? (computed[computed.length - 1]!.x +
            computed[0]!.groupW)
        : containerWidth;

    const labelStep = Math.max(
      1,
      Math.floor(groups.length / 10),
    );

    return { groups: computed, maxVal, svgW, labelStep };
  });

  // --- Sessions table sorting ---
  type SortCol =
    | "timestamp"
    | "turn_count"
    | "total_tokens"
    | "average_tps"
    | "average_otps";
  let sortCol = $state<SortCol>("timestamp");
  let sortDesc = $state(true);

  const sortedSessions = $derived.by(() => {
    if (!tps) return [];
    const rows = [...tps.sessions];
    rows.sort((a, b) => {
      let cmp = 0;
      switch (sortCol) {
        case "timestamp":
          cmp = a.timestamp.localeCompare(b.timestamp);
          break;
        case "turn_count":
          cmp = a.turn_count - b.turn_count;
          break;
        case "total_tokens":
          cmp = a.total_tokens - b.total_tokens;
          break;
        case "average_tps":
          cmp = a.average_tps - b.average_tps;
          break;
        case "average_otps":
          cmp = a.average_otps - b.average_otps;
          break;
      }
      return sortDesc ? -cmp : cmp;
    });
    return rows;
  });

  function toggleSort(col: SortCol) {
    if (sortCol === col) {
      sortDesc = !sortDesc;
    } else {
      sortCol = col;
      sortDesc = true;
    }
  }
</script>

<div class="tps-dashboard">
  <h3 class="section-title">Tokens Per Second</h3>

  {#if analytics.errors.tps}
    <div class="error">
      {analytics.errors.tps}
      <button class="retry-btn" onclick={() => analytics.fetchTPS()}>
        Retry
      </button>
    </div>
  {:else if tps}
    {@const o = tps.overview}

    {#if o.total_turns === 0}
      <div class="empty">
        No TPS data available for the selected sessions.
        <br />
        <span class="empty-hint">
          TPS requires sessions with per-message token_usage and model
          data (Claude, Codex, Gemini, OpenCode, Qwen, QClaw, Pi,
          OpenClaw, WorkBuddy).
        </span>
      </div>
    {:else}
      <!-- Summary cards -->
      <div class="metrics-grid">
        <div class="metric-card">
          <div class="metric-label">Sessions</div>
          <div class="metric-value">{fmtInt(o.total_sessions)}</div>
        </div>
        <div class="metric-card">
          <div class="metric-label">Turns</div>
          <div class="metric-value">{fmtInt(o.total_turns)}</div>
        </div>
        <div class="metric-card">
          <div class="metric-label">Avg TPS</div>
          <div class="metric-value">{fmt(o.average_tps, 1)}</div>
        </div>
        <div class="metric-card">
          <div class="metric-label">Avg ITPS</div>
          <div class="metric-value">{fmt(o.average_itps, 1)}</div>
        </div>
        <div class="metric-card">
          <div class="metric-label">Avg OTPS</div>
          <div class="metric-value">{fmt(o.average_otps, 1)}</div>
        </div>
        <div class="metric-card">
          <div class="metric-label">Total Tokens</div>
          <div class="metric-value">{fmtInt(o.total_tokens)}</div>
        </div>
      </div>

      <!-- Percentile cards -->
      <div class="pct-grid">
        <div class="pct-card">
          <div class="pct-label">TPS p50/p75/p90/p95/max</div>
          <div class="pct-value">{fmtPct(o.tps_percentiles)}</div>
        </div>
        <div class="pct-card">
          <div class="pct-label">ITPS p50/p75/p90/p95/max</div>
          <div class="pct-value">{fmtPct(o.itps_percentiles)}</div>
        </div>
        <div class="pct-card">
          <div class="pct-label">OTPS p50/p75/p90/p95/max</div>
          <div class="pct-value">{fmtPct(o.otps_percentiles)}</div>
        </div>
      </div>

      <!-- Chart -->
      <div class="chart-section">
        <div class="chart-header">
          <span class="chart-subtitle">
            TPS / ITPS / OTPS by {PERIODS.find((p) => p.value === activePeriod)?.label}
          </span>
          <div class="period-tabs">
            {#each PERIODS as p}
              <button
                class="period-btn"
                class:active={activePeriod === p.value}
                onclick={() => (activePeriod = p.value)}
              >
                {p.label}
              </button>
            {/each}
          </div>
        </div>
        <div class="chart-container" bind:this={containerEl}>
          {#if chart.groups.length > 0}
            <svg
              class="chart-svg"
              width={chart.svgW}
              height={CHART_H}
            >
              {#each chart.groups as g, i}
                {@const baseY = TOP_PAD + BAR_HEIGHT}
                {@const groupInnerW = g.width * 3 + BAR_GAP * 2}
                <rect
                  class="bar bar-tps"
                  x={g.x}
                  y={baseY - g.tpsH}
                  width={g.width}
                  height={g.tpsH}
                />
                <rect
                  class="bar bar-itps"
                  x={g.x + g.width + BAR_GAP}
                  y={baseY - g.itpsH}
                  width={g.width}
                  height={g.itpsH}
                />
                <rect
                  class="bar bar-otps"
                  x={g.x + (g.width + BAR_GAP) * 2}
                  y={baseY - g.otpsH}
                  width={g.width}
                  height={g.otpsH}
                />
                {#if i % chart.labelStep === 0}
                  <text
                    class="axis-label"
                    x={g.x + groupInnerW / 2}
                    y={CHART_H - 6}
                    text-anchor="middle"
                  >
                    {g.label}
                  </text>
                {/if}
              {/each}
            </svg>
          {:else}
            <div class="empty small">No chart data</div>
          {/if}
        </div>
        <div class="legend">
          <span class="legend-item"><span class="dot tps"></span> TPS</span>
          <span class="legend-item"><span class="dot itps"></span> ITPS</span>
          <span class="legend-item"><span class="dot otps"></span> OTPS</span>
        </div>
      </div>

      <!-- Model statistics -->
      {#if tps.by_model.length > 0}
        <div class="model-section">
          <div class="chart-subtitle">Model Statistics</div>
          <div class="model-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th class="col-name">Model</th>
                  <th>Tokens</th>
                  <th>Input</th>
                  <th>Output</th>
                  <th>Turns</th>
                  <th>Avg TPS</th>
                  <th>TPS p50/75/90/95/max</th>
                  <th>OTPS p50/75/90/95/max</th>
                  <th>Duration</th>
                </tr>
              </thead>
              <tbody>
                {#each tps.by_model as m}
                  <tr>
                    <td class="col-name" title={m.model}>{m.model}</td>
                    <td class="num">{fmtInt(m.total_tokens)}</td>
                    <td class="num">{fmtInt(m.total_input_tokens)}</td>
                    <td class="num">{fmtInt(m.total_output_tokens)}</td>
                    <td class="num">{fmtInt(m.turn_count)}</td>
                    <td class="num">{fmt(m.average_tps, 1)}</td>
                    <td class="num pct">{fmtPct(m.tps_percentiles)}</td>
                    <td class="num pct">{fmtPct(m.otps_percentiles)}</td>
                    <td class="num">{fmtDuration(m.total_duration_seconds)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </div>
      {/if}

      <!-- Sessions table -->
      {#if tps.sessions.length > 0}
        <div class="sessions-section">
          <div class="chart-subtitle">
            Sessions ({fmtInt(tps.sessions.length)})
          </div>
          <div class="model-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th class="col-name">Session</th>
                  <th>
                    <button class="th-btn" onclick={() => toggleSort("timestamp")}>
                      Date
                    </button>
                  </th>
                  <th>
                    <button class="th-btn" onclick={() => toggleSort("turn_count")}>
                      Turns
                    </button>
                  </th>
                  <th>
                    <button class="th-btn" onclick={() => toggleSort("total_tokens")}>
                      Tokens
                    </button>
                  </th>
                  <th>Input</th>
                  <th>Output</th>
                  <th>
                    <button class="th-btn" onclick={() => toggleSort("average_tps")}>
                      TPS
                    </button>
                  </th>
                  <th>ITPS</th>
                  <th>
                    <button class="th-btn" onclick={() => toggleSort("average_otps")}>
                      OTPS
                    </button>
                  </th>
                  <th class="col-name">Models</th>
                </tr>
              </thead>
              <tbody>
                {#each sortedSessions.slice(0, 50) as s}
                  <tr
                    class="clickable"
                    onclick={() => router.navigateToSession(s.session_id)}
                  >
                    <td class="col-name mono" title={s.session_id}>
                      {s.session_id.slice(0, 12)}
                    </td>
                    <td class="num">
                      {new Date(s.timestamp).toLocaleDateString("en", {
                        month: "short",
                        day: "numeric",
                      })}
                    </td>
                    <td class="num">{s.turn_count}</td>
                    <td class="num">{fmtInt(s.total_tokens)}</td>
                    <td class="num">{fmtInt(s.input_tokens)}</td>
                    <td class="num">{fmtInt(s.output_tokens)}</td>
                    <td class="num">{fmt(s.average_tps, 1)}</td>
                    <td class="num">{fmt(s.average_itps, 1)}</td>
                    <td class="num">{fmt(s.average_otps, 1)}</td>
                    <td class="col-name" title={s.models.join(", ")}>
                      {s.models.length > 1
                        ? `${s.models.length} models`
                        : s.models[0] || "unknown"}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
          {#if tps.sessions.length > 50}
            <div class="table-footer">
              Showing 50 of {fmtInt(tps.sessions.length)} sessions
            </div>
          {/if}
        </div>
      {/if}
    {/if}
  {:else}
    <div class="empty">Loading TPS data...</div>
  {/if}
</div>

<style>
  .tps-dashboard {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .section-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
  }

  .metrics-grid {
    display: grid;
    grid-template-columns: repeat(6, 1fr);
    gap: 8px;
  }

  @media (max-width: 900px) {
    .metrics-grid {
      grid-template-columns: repeat(3, 1fr);
    }
  }

  .metric-card {
    padding: 8px;
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
    text-align: center;
  }

  .metric-label {
    font-size: 9px;
    color: var(--text-muted);
    margin-bottom: 4px;
  }

  .metric-value {
    font-size: 16px;
    font-weight: 600;
    color: var(--text-primary);
    font-variant-numeric: tabular-nums;
  }

  .pct-grid {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 8px;
  }

  @media (max-width: 900px) {
    .pct-grid {
      grid-template-columns: 1fr;
    }
  }

  .pct-card {
    padding: 8px;
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
  }

  .pct-label {
    font-size: 9px;
    color: var(--text-muted);
    margin-bottom: 4px;
  }

  .pct-value {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
  }

  .chart-section {
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
    padding: 12px;
  }

  .chart-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
    flex-wrap: wrap;
    gap: 8px;
  }

  .chart-subtitle {
    font-size: 11px;
    font-weight: 600;
    color: var(--text-secondary);
  }

  .period-tabs {
    display: flex;
    gap: 2px;
    flex-wrap: wrap;
  }

  .period-btn {
    height: 22px;
    padding: 0 6px;
    border-radius: var(--radius-sm);
    font-size: 10px;
    font-weight: 500;
    color: var(--text-muted);
    cursor: pointer;
    transition: background 0.1s, color 0.1s;
  }

  .period-btn:hover {
    background: var(--bg-surface-hover);
    color: var(--text-secondary);
  }

  .period-btn.active {
    background: var(--bg-surface);
    color: var(--text-primary);
  }

  .chart-container {
    width: 100%;
    overflow-x: auto;
  }

  .chart-svg {
    display: block;
  }

  .bar {
    rx: 1;
  }

  .bar-tps {
    fill: rgba(39, 174, 96, 0.75);
  }

  .bar-itps {
    fill: rgba(52, 152, 219, 0.75);
  }

  .bar-otps {
    fill: rgba(155, 89, 182, 0.75);
  }

  .axis-label {
    font-size: 9px;
    fill: var(--text-muted);
  }

  .legend {
    display: flex;
    gap: 12px;
    margin-top: 8px;
    font-size: 10px;
    color: var(--text-muted);
  }

  .legend-item {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 2px;
    display: inline-block;
  }

  .dot.tps {
    background: rgba(39, 174, 96, 0.75);
  }

  .dot.itps {
    background: rgba(52, 152, 219, 0.75);
  }

  .dot.otps {
    background: rgba(155, 89, 182, 0.75);
  }

  .model-section,
  .sessions-section {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .model-table-wrap {
    overflow-x: auto;
  }

  .data-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 11px;
  }

  .data-table th {
    text-align: right;
    font-weight: 500;
    font-size: 9px;
    color: var(--text-muted);
    padding: 4px 6px;
    border-bottom: 1px solid var(--border-muted);
    white-space: nowrap;
  }

  .data-table th.col-name {
    text-align: left;
  }

  .data-table td {
    padding: 4px 6px;
    color: var(--text-secondary);
    border-bottom: 1px solid var(--border-muted);
    white-space: nowrap;
  }

  .data-table td.col-name {
    text-align: left;
    max-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .data-table td.num {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  .data-table td.pct {
    font-size: 10px;
  }

  .data-table td.mono {
    font-family: var(--font-mono, monospace);
    font-size: 10px;
  }

  .data-table tbody tr:hover {
    background: var(--bg-surface-hover);
  }

  .data-table tbody tr.clickable {
    cursor: pointer;
  }

  .th-btn {
    font: inherit;
    color: inherit;
    background: none;
    border: none;
    cursor: pointer;
    padding: 0;
    text-decoration: underline;
    text-decoration-color: transparent;
  }

  .th-btn:hover {
    text-decoration-color: var(--text-muted);
  }

  .table-footer {
    font-size: 10px;
    color: var(--text-muted);
    padding: 4px 0;
  }

  .empty {
    color: var(--text-muted);
    font-size: 12px;
    padding: 24px;
    text-align: center;
    line-height: 1.6;
  }

  .empty.small {
    padding: 12px;
  }

  .empty-hint {
    font-size: 10px;
    color: var(--text-muted);
    opacity: 0.7;
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
