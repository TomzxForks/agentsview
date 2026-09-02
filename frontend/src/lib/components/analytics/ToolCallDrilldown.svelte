<!-- ABOUTME: Per-tool drill-down modal — every matching call across all
     sessions, grouped per session with per-call durations. Opened from
     the Tool Usage card's tool names. Mirrors the analysis sidebar's
     calls list look via the shared CallRow component. The session/call
     list is fully virtualized (one row per header or call) so large
     result sets render only the visible window. -->
<script lang="ts">
  import { onDestroy } from "svelte";
  import type { Virtualizer } from "@tanstack/virtual-core";
  import { Modal, Spinner } from "@kenn-io/kit-ui";
  import { m } from "../../i18n/index.js";
  import { analytics } from "../../stores/analytics.svelte.js";
  import { router } from "../../stores/router.svelte.js";
  import { ui } from "../../stores/ui.svelte.js";
  import { AnalyticsService } from "../../api/generated/index.js";
  import { callGenerated, isAbortError } from "../../api/runtime.js";
  import { formatDuration } from "../../utils/duration.js";
  import { formatNumber, truncate } from "../../utils/format.js";
  import { normalizeMessagePreview } from "../../utils/messages.js";
  import type {
    ToolCallSessionGroup,
    ToolCallTiming,
    ToolCallsResponse,
  } from "../../api/types/analytics.js";
  import type { CallTiming } from "../../api/types/timing.js";
  import { createVirtualizer } from "../../virtual/createVirtualizer.svelte.js";
  import CallRow from "../content/CallRow.svelte";

  interface Props {
    toolName: string;
    category: string;
    onclose: () => void;
  }

  let { toolName, category, onclose }: Props = $props();

  let data = $state<ToolCallsResponse | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);

  let abortController: AbortController | undefined;

  async function fetchCalls() {
    abortController?.abort();
    const controller = new AbortController();
    abortController = controller;
    loading = true;
    error = null;
    try {
      const params = {
        ...analytics.toolUsageDrilldownParams(),
        toolName,
        category,
      };
      const result = await callGenerated(
        () =>
          AnalyticsService.getApiV1AnalyticsToolsCalls(
            params,
          ) as unknown as Promise<ToolCallsResponse>,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      data = result;
    } catch (e) {
      if (isAbortError(e) || controller.signal.aborted) return;
      error = e instanceof Error ? e.message : "Failed to load";
    } finally {
      if (!controller.signal.aborted) loading = false;
    }
  }

  $effect(() => {
    void fetchCalls();
    return () => abortController?.abort();
  });
  onDestroy(() => abortController?.abort());

  // Bar widths scale against the slowest call in the drill-down so
  // call-vs-call comparisons stay legible, like the per-session calls
  // list in the analysis sidebar.
  let maxCallMs = $derived.by(() => {
    let max = 0;
    for (const g of data?.sessions ?? []) {
      for (const call of g.calls) {
        if (call.duration_ms != null && call.duration_ms > max) {
          max = call.duration_ms;
        }
      }
    }
    return max;
  });

  /** Calls actually listed — the server caps the list, totals don't. */
  let shownCallCount = $derived.by(() => {
    let total = 0;
    for (const g of data?.sessions ?? []) {
      total += g.calls.length;
    }
    return total;
  });

  // Flat virtualizer rows: one per session header or call, so a
   // result with thousands of entries only renders the visible window.
  type DrilldownRow =
    | {
        kind: "header";
        key: string;
        group: ToolCallSessionGroup;
        groupEnd: boolean;
      }
    | {
        kind: "call";
        key: string;
        group: ToolCallSessionGroup;
        call: ToolCallTiming;
        groupEnd: boolean;
      };

  let rows = $derived.by(() => {
    const out: DrilldownRow[] = [];
    for (const g of data?.sessions ?? []) {
      out.push({
        kind: "header",
        key: `h-${g.session_id}`,
        group: g,
        groupEnd: g.calls.length === 0,
      });
      for (let i = 0; i < g.calls.length; i++) {
        out.push({
          kind: "call",
          key: `c-${g.session_id}-${i}`,
          group: g,
          call: g.calls[i]!,
          groupEnd: i === g.calls.length - 1,
        });
      }
    }
    return out;
  });

  let groupsEl = $state<HTMLElement | undefined>(undefined);

  const virtualizer = createVirtualizer(() => {
    return {
      count: rows.length,
      getScrollElement: () => groupsEl ?? null,
      estimateSize: (index) =>
        rows[index]?.kind === "header" ? 38 : 24,
      overscan: 8,
      measureCacheKey: data,
      getItemKey: (index: number) => rows[index]?.key ?? `idx-${index}`,
    };
  });

  /** Svelte action: measure element for variable-height virtualizer */
  function measureElement(
    node: HTMLElement,
    virt: Virtualizer<HTMLElement, HTMLElement> | undefined,
  ) {
    virt?.measureElement(node);
    return {
      update(nextVirt: Virtualizer<HTMLElement, HTMLElement> | undefined) {
        nextVirt?.measureElement(node);
      },
      destroy() {
        // Cleanup handled by virtualizer
      },
    };
  }

  function callBarPct(call: ToolCallTiming): number {
    if (call.duration_ms == null || call.duration_ms <= 0 || maxCallMs <= 0) {
      return 0;
    }
    return Math.min(
      100,
      Math.max((call.duration_ms / maxCallMs) * 100, 4),
    );
  }

  function toCallTiming(call: ToolCallTiming): CallTiming {
    const adapted: CallTiming = {
      tool_use_id: call.tool_use_id,
      tool_name: toolName,
      category: call.category,
      duration_ms: call.duration_ms,
      is_parallel: false,
      input_preview: call.input_preview,
    };
    if (call.skill_name != null) adapted.skill_name = call.skill_name;
    if (call.subagent_session_id != null) {
      adapted.subagent_session_id = call.subagent_session_id;
    }
    return adapted;
  }

  function sessionLabel(g: ToolCallSessionGroup): string {
    return (
      g.display_name ||
      normalizeMessagePreview(g.first_message ?? null) ||
      g.session_id.slice(0, 12)
    );
  }

  function openSession(g: ToolCallSessionGroup) {
    onclose();
    router.navigateToSession(g.session_id);
  }

  function openCall(g: ToolCallSessionGroup, call: ToolCallTiming) {
    onclose();
    router.navigateToSession(g.session_id);
    ui.scrollToOrdinal(call.message_ordinal, g.session_id);
  }
</script>

<Modal
  title={m.analytics_tool_calls_title({ tool: toolName })}
  closeLabel={m.analytics_tool_calls_close()}
  width="720px"
  maxWidth="calc(100vw - 32px)"
  {onclose}
>
  {#if data}
    <div class="summary">
      <span>
        {m.analytics_tool_calls_call_count({
          count: data.total_calls,
          countLabel: formatNumber(data.total_calls),
        })}
      </span>
      <span class="sep">·</span>
      <span>
        {m.analytics_tool_calls_total_duration({
          duration: formatDuration(data.total_duration_ms),
        })}
      </span>
      <span class="sep">·</span>
      <span>
        {m.analytics_tool_calls_session_count({
          count: data.session_count,
          countLabel: formatNumber(data.session_count),
        })}
      </span>
    </div>
    {#if data.truncated}
      <div class="truncated">
        {m.analytics_tool_calls_truncated({
          count: shownCallCount,
          countLabel: formatNumber(shownCallCount),
          totalCount: data.total_calls,
          totalCountLabel: formatNumber(data.total_calls),
        })}
      </div>
    {/if}
  {/if}

  {#if loading}
    <div class="state"><Spinner /></div>
  {:else if error}
    <div class="state error">
      {error}
      <button class="retry-btn" onclick={() => void fetchCalls()}>
        {m.shared_retry()}
      </button>
    </div>
  {:else if !data || data.sessions.length === 0}
    <div class="state">{m.analytics_tool_calls_empty()}</div>
  {:else}
    <div class="groups" bind:this={groupsEl}>
      <div
        class="groups-inner"
        style="height: {virtualizer.instance?.getTotalSize() ?? 0}px;"
      >
        {#each virtualizer.instance?.getVirtualItems() ?? [] as row (row.key)}
          {@const item = rows[row.index]}
          {#if item}
            <div
              class="vrow"
              data-index={row.index}
              style="transform: translateY({row.start}px);"
              use:measureElement={virtualizer.instance}
            >
              {#if item.kind === "header"}
                <button
                  type="button"
                  class="group-header"
                  class:group-end={item.groupEnd}
                  title={m.analytics_tool_calls_open_session()}
                  onclick={() => openSession(item.group)}
                >
                  <span class="g-label">
                    {truncate(sessionLabel(item.group), 48)}
                  </span>
                  <span class="g-project">{item.group.project}</span>
                  <span class="g-meta">
                    {m.analytics_tool_calls_call_count({
                      count: item.group.call_count,
                      countLabel: formatNumber(item.group.call_count),
                    })}
                  </span>
                  <span class="g-dur">
                    {formatDuration(item.group.total_duration_ms)}
                  </span>
                </button>
              {:else}
                <div class="call-slot" class:group-end={item.groupEnd}>
                  <CallRow
                    call={toCallTiming(item.call)}
                    barWidthPct={callBarPct(item.call)}
                    expandable={false}
                    onClick={() => openCall(item.group, item.call)}
                  />
                </div>
              {/if}
            </div>
          {/if}
        {/each}
      </div>
    </div>
  {/if}
</Modal>

<style>
  .summary {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 11px;
    font-family: var(--font-mono);
    color: var(--text-secondary);
    margin-bottom: 4px;
  }
  .summary .sep {
    color: var(--text-muted);
  }
  .truncated {
    font-size: 10px;
    color: var(--text-muted);
    margin-bottom: 4px;
  }
  .state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 32px 0;
    font-size: 12px;
    color: var(--text-muted);
  }
  .state.error {
    color: var(--accent-red);
  }
  .retry-btn {
    padding: 2px 8px;
    border: 1px solid currentColor;
    border-radius: var(--radius-sm);
    font-size: 11px;
    color: inherit;
    cursor: pointer;
  }
  .groups {
    position: relative;
    overflow-y: auto;
    /* Fixed height, not max-height: the virtualizer measures this
     * element as its viewport, and an empty max-height box collapses
     * to 0px, which would render zero rows (deadlock). */
    height: min(60vh, 520px);
    margin-top: 8px;
  }
  .groups-inner {
    position: relative;
    width: 100%;
  }
  .vrow {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
  }
  .group-header {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto 64px;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 6px 8px;
    margin-top: 8px;
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    border-bottom: 0;
    border-radius: var(--radius-sm) var(--radius-sm) 0 0;
    text-align: left;
    cursor: pointer;
    transition: background 0.1s;
  }
  .group-header.group-end {
    border-bottom: 1px solid var(--border-muted);
    border-radius: var(--radius-sm);
  }
  .group-header:hover {
    background: var(--bg-surface-hover);
  }
  .group-header:focus-visible {
    outline: 2px solid var(--accent-blue);
    outline-offset: -2px;
  }
  .g-label {
    font-size: 11px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .g-project {
    font-size: 9px;
    font-family: var(--font-mono);
    color: var(--text-muted);
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .g-meta {
    font-size: 10px;
    font-family: var(--font-mono);
    color: var(--text-muted);
    white-space: nowrap;
  }
  .g-dur {
    font-size: 11px;
    font-family: var(--font-mono);
    color: var(--accent-blue);
    text-align: right;
    white-space: nowrap;
  }
  .call-slot {
    border-left: 1px solid var(--border-muted);
    border-right: 1px solid var(--border-muted);
  }
  .call-slot.group-end {
    border-bottom: 1px solid var(--border-muted);
    border-radius: 0 0 var(--radius-sm) var(--radius-sm);
    padding-bottom: 3px;
  }
</style>
