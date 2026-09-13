// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { mount, tick, unmount } from "svelte";
// @ts-ignore
import ToolUsage from "./ToolUsage.svelte";
import { analytics } from "../../stores/analytics.svelte.js";
// @ts-ignore
import { AnalyticsService } from "../../api/generated/index.js";

// jsdom has no layout, so the real TanStack virtualizer would render a
// zero-height window. Render every row instead.
vi.mock("../../virtual/createVirtualizer.svelte.js", () => ({
  createVirtualizer: (optsFn: () => any) => ({
    instance: {
      getVirtualItems: () => {
        const opts = optsFn();
        return Array.from({ length: opts.count }, (_, index) => ({
          index,
          key: opts.getItemKey(index),
          start: index * 24,
          end: (index + 1) * 24,
          size: 24,
        }));
      },
      getTotalSize: () => optsFn().count * 24,
      measureElement: () => {},
    },
  }),
}));

function requireNonNull<T>(value: T | null | undefined, label: string): T {
  if (value == null) throw new Error(`missing ${label}`);
  return value;
}

describe("ToolUsage", () => {
  afterEach(() => {
    analytics.tools = null;
    // @ts-ignore
    analytics.errors = {
      ...analytics.errors,
      tools: null,
    };
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  it("renders ranked per-tool analysis rows", async () => {
    analytics.tools = {
      total_calls: 6,
      by_category: [
        { category: "Read", count: 3, pct: 50 },
        { category: "Bash", count: 2, pct: 33.3 },
      ],
      by_agent: [],
      by_tool: [
        {
          tool_name: "Read",
          category: "Read",
          call_count: 3,
          total_duration_ms: 4500,
          session_count: 2,
          pct: 50,
        },
        {
          tool_name: "Bash",
          category: "Bash",
          call_count: 2,
          total_duration_ms: 1200,
          session_count: 1,
          pct: 33.3,
        },
      ],
      trend: [
        {
          date: "2024-06-03",
          by_category: { Read: 3, Bash: 2 },
        },
        {
          date: "2024-06-10",
          by_category: { Read: 1 },
        },
      ],
    };

    const component = mount(ToolUsage, { target: document.body });
    await tick();

    expect(document.body.textContent).toContain("Tool Usage");
    expect(document.body.textContent).toContain("6 calls");
    expect(document.body.textContent).toContain("Top tools");
    expect(document.body.textContent).toContain("Read");
    expect(document.body.textContent).toContain("3");
    expect(document.body.textContent).toContain("4.5s");
    expect(document.body.textContent).toContain("1.2s");
    expect(document.body.textContent).toContain("2 sessions");
    expect(document.body.textContent).toContain("50%");
    expect(document.body.textContent).toContain("Bash");
    expect(document.body.textContent).toContain("1 session");
    expect(document.body.textContent).not.toContain("1 sessions");
    expect(document.body.textContent).toContain("33.3%");
    expect(document.body.textContent).toContain("By Category");
    expect(document.body.textContent).toContain("Weekly Trend");

    unmount(component);
  });

  it("opens the per-tool call drilldown from a tool name", async () => {
    const callsSpy = vi.spyOn(AnalyticsService, "getApiV1AnalyticsToolsCalls").mockResolvedValue({
      tool_name: "Read",
      category: "Read",
      total_calls: 2,
      total_duration_ms: 5000,
      session_count: 1,
      truncated: false,
      sessions: [
        {
          session_id: "s1",
          project: "alpha",
          agent: "claude",
          started_at: "2024-06-01T09:00:00Z",
          display_name: "Alpha session",
          first_message: null,
          call_count: 2,
          total_duration_ms: 5000,
          calls: [
            {
              tool_use_id: "tu_1",
              category: "Read",
              duration_ms: 3000,
              started_at: "2024-06-01T09:00:01Z",
              message_ordinal: 1,
              input_preview: "a.go",
            },
            {
              tool_use_id: "tu_2",
              category: "Read",
              duration_ms: 2000,
              started_at: "2024-06-01T09:00:02Z",
              message_ordinal: 2,
              input_preview: "b.go",
            },
          ],
        },
      ],
    });
    analytics.tools = {
      total_calls: 3,
      by_category: [{ category: "Read", count: 3, pct: 100 }],
      by_agent: [],
      by_tool: [
        {
          tool_name: "Read",
          category: "Read",
          call_count: 3,
          total_duration_ms: 5000,
          session_count: 2,
          pct: 100,
        },
      ],
      trend: [],
    };

    const component = mount(ToolUsage, { target: document.body });
    await tick();

    const toolNameButton = requireNonNull(
      document.body.querySelector<HTMLButtonElement>(".tool-name"),
      "tool name button",
    );
    toolNameButton.click();
    await vi.waitFor(() => {
      expect(document.body.textContent).toContain("Alpha session");
    });

    expect(callsSpy).toHaveBeenCalled();
    expect(document.body.textContent).toContain("Tool calls: Read");
    expect(document.body.textContent).toContain("2 calls");
    expect(document.body.textContent).toContain("Total time 5.0s");
    expect(document.body.textContent).toContain("1 session");
    expect(document.body.textContent).toContain("Alpha session");
    expect(document.body.textContent).toContain("3.0s");
    expect(document.body.textContent).toContain("2.0s");

    unmount(component);
  });

  it("renders loading state while the first fetch is in flight", async () => {
    analytics.tools = null;
    analytics.loading = { ...analytics.loading, tools: true };

    const component = mount(ToolUsage, { target: document.body });
    await tick();

    expect(document.body.textContent).toContain("Loading tool usage...");
    expect(document.body.textContent).not.toContain("No tool usage data");

    analytics.loading = { ...analytics.loading, tools: false };
    unmount(component);
  });

  it("renders empty state without tool rows", async () => {
    analytics.tools = {
      total_calls: 0,
      by_category: [],
      by_agent: [],
      by_tool: [],
      trend: [],
    };

    const component = mount(ToolUsage, { target: document.body });
    await tick();

    expect(document.body.textContent).toContain("No tool usage data");

    unmount(component);
  });
});
