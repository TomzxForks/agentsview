// @vitest-environment jsdom
import {
  afterEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import { mount, tick, unmount } from "svelte";
// @ts-ignore
import SkillTrend from "./SkillTrend.svelte";
import { analytics } from "../../stores/analytics.svelte.js";

describe("SkillTrend", () => {
  afterEach(() => {
    analytics.skills = null;
    // @ts-ignore
    analytics.errors = {
      ...analytics.errors,
      skills: null,
    };
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  function skillsResponse(
    trend: { date: string; by_skill: Record<string, number> }[],
  ) {
    return {
      total_skill_calls: 0,
      distinct_skills: 0,
      by_skill: [],
      trend,
    };
  }

  function mountWithData() {
    analytics.skills = skillsResponse([
      {
        date: "2024-01-01",
        by_skill: { commit: 4, review: 2 },
      },
      {
        date: "2024-01-08",
        by_skill: { commit: 6, deploy: 1 },
      },
    ]);
    // @ts-ignore
    analytics.errors = {
      ...analytics.errors,
      skills: null,
    };

    return mount(SkillTrend, { target: document.body });
  }

  it("renders stacked weekly columns with a legend", async () => {
    const component = mountWithData();
    await tick();

    expect(document.body.textContent).toContain("Skill Usage Over Time");
    expect(document.body.textContent).toContain("Weekly Trend");

    const chips = document.querySelectorAll<HTMLButtonElement>(
      ".legend-chip",
    );
    expect(chips).toHaveLength(3);
    // Legend ordered by total volume: commit (10), review (2), deploy (1).
    expect(chips[0]!.textContent).toContain("commit");
    expect(chips[0]!.textContent).toContain("10");
    expect(chips[1]!.textContent).toContain("review");
    expect(chips[2]!.textContent).toContain("deploy");

    const columns = document.querySelectorAll(".week-column");
    expect(columns).toHaveLength(2);
    expect(columns[0]!.querySelectorAll(".segment")).toHaveLength(2);
    expect(columns[1]!.querySelectorAll(".segment")).toHaveLength(2);
    expect(document.body.textContent).toContain("01-01");
    expect(document.body.textContent).toContain("01-08");

    unmount(component);
  });

  it("hides a series when its legend chip is toggled", async () => {
    const component = mountWithData();
    await tick();

    const chips = document.querySelectorAll<HTMLButtonElement>(
      ".legend-chip",
    );
    expect(chips[0]!.getAttribute("aria-pressed")).toBe("true");
    chips[0]!.click();
    await tick();

    expect(chips[0]!.getAttribute("aria-pressed")).toBe("false");
    const columns = document.querySelectorAll(".week-column");
    // "commit" segments are gone; the other series remain.
    expect(columns[0]!.querySelectorAll(".segment")).toHaveLength(1);
    expect(columns[1]!.querySelectorAll(".segment")).toHaveLength(1);

    chips[0]!.click();
    await tick();
    expect(
      document
        .querySelectorAll(".week-column")[0]!
        .querySelectorAll(".segment"),
    ).toHaveLength(2);

    unmount(component);
  });

  it("keeps survivor colors stable when a series is hidden", async () => {
    const component = mountWithData();
    await tick();

    const firstWeekSegments = () => [
      ...document
        .querySelectorAll(".week-column")[0]!
        .querySelectorAll<HTMLElement>(".segment"),
    ];
    // "review" is the top segment of the first week (series slot 2).
    const before = firstWeekSegments().at(-1)!.getAttribute("style");
    expect(before).toContain("--chart-series-2");

    document
      .querySelectorAll<HTMLButtonElement>(".legend-chip")[0]!
      .click();
    await tick();

    // With "commit" hidden, "review" keeps its slot-2 hue.
    const after = firstWeekSegments().at(-1)!.getAttribute("style");
    expect(after).toContain("--chart-series-2");

    unmount(component);
  });

  it("folds skills past the series cap into Other", async () => {
    const bySkill: Record<string, number> = {};
    for (let i = 0; i < 8; i++) {
      bySkill[`skill-${i}`] = 8 - i;
    }
    analytics.skills = skillsResponse([
      { date: "2024-01-01", by_skill: bySkill },
    ]);
    const component = mount(SkillTrend, { target: document.body });
    await tick();

    const chips = document.querySelectorAll<HTMLButtonElement>(
      ".legend-chip",
    );
    expect(chips).toHaveLength(7);
    expect(chips[6]!.textContent).toContain("Other");
    // skill-6 (2 calls) + skill-7 (1 call) fold into Other.
    expect(chips[6]!.textContent).toContain("3");

    const segments = document
      .querySelectorAll(".week-column")[0]!
      .querySelectorAll(".segment");
    expect(segments).toHaveLength(7);

    unmount(component);
  });

  it("renders empty state", async () => {
    analytics.skills = skillsResponse([]);
    const component = mount(SkillTrend, { target: document.body });
    await tick();

    expect(document.body.textContent).toContain("No skill usage data");

    unmount(component);
  });

  it("renders error state and retries", async () => {
    analytics.skills = null;
    // @ts-ignore
    analytics.errors = {
      ...analytics.errors,
      skills: "Failed to load",
    };
    const retrySpy = vi
      .spyOn(analytics, "fetchSkills")
      .mockResolvedValue("ok");
    const component = mount(SkillTrend, { target: document.body });
    await tick();

    expect(document.body.textContent).toContain("Failed to load");
    document.querySelector<HTMLButtonElement>(".retry-btn")!.click();
    await tick();

    expect(retrySpy).toHaveBeenCalledOnce();

    unmount(component);
  });
});
