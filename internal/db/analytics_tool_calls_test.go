package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedToolDurationFixture seeds two sessions whose Read calls carry
// measurable durations (one from tool_execution events, one from a
// sub-agent session) plus one parallel call with no measurable
// duration and an off-tool Bash call.
func seedToolDurationFixture(t *testing.T, d *DB) {
	t.Helper()

	// Child sub-agent session: 10s wall time.
	insertSession(t, d, "dur-child", "alpha", func(s *Session) {
		s.StartedAt = new("2024-06-01T10:00:04Z")
		s.EndedAt = new("2024-06-01T10:00:14Z")
		s.Agent = "claude"
	})

	insertSession(t, d, "dur-a", "alpha", func(s *Session) {
		s.StartedAt = new("2024-06-01T10:00:00Z")
		s.EndedAt = new("2024-06-01T10:05:00Z")
		s.MessageCount = 2
		s.Agent = "claude"
		s.SessionName = new("Alpha duration session")
	})
	ma := asstMsgAt("dur-a", 0, "[Read: a.go, Read: b.go]",
		"2024-06-01T10:00:01Z")
	ma.HasToolUse = true
	ma.ToolCalls = []ToolCall{
		{
			SessionID: "dur-a", ToolName: "Read", Category: "Read",
			ToolUseID: "tu_a1", InputJSON: `{"file_path":"a.go"}`,
			ResultEvents: []ToolResultEvent{
				{Source: "tool_execution", Status: "started",
					Timestamp: "2024-06-01T10:00:01Z"},
				{Source: "tool_execution", Status: "completed",
					Timestamp: "2024-06-01T10:00:03.5Z"},
			},
		},
		{
			// Parallel sibling without event coverage: no
			// measurable duration.
			SessionID: "dur-a", ToolName: "Read", Category: "Read",
			ToolUseID: "tu_a2", InputJSON: `{"file_path":"b.go"}`,
		},
	}
	mb := asstMsgAt("dur-a", 1, "[Task: child]",
		"2024-06-01T10:00:04Z")
	mb.HasToolUse = true
	mb.ToolCalls = []ToolCall{
		{
			SessionID: "dur-a", ToolName: "Task", Category: "Task",
			ToolUseID: "tu_sub", SubagentSessionID: "dur-child",
		},
	}
	insertMessages(t, d, ma, mb)

	insertSession(t, d, "dur-b", "beta", func(s *Session) {
		s.StartedAt = new("2024-06-02T11:00:00Z")
		s.EndedAt = new("2024-06-02T11:01:00Z")
		s.MessageCount = 1
		s.Agent = "codex"
	})
	mc := asstMsgAt("dur-b", 0, "[Read: c.go, Bash: ls]",
		"2024-06-02T11:00:00Z")
	mc.HasToolUse = true
	mc.ToolCalls = []ToolCall{
		{
			SessionID: "dur-b", ToolName: "Read", Category: "Read",
			ToolUseID: "tu_b1", InputJSON: `{"file_path":"c.go"}`,
			ResultEvents: []ToolResultEvent{
				{Source: "tool_execution", Status: "started",
					Timestamp: "2024-06-02T11:00:00Z"},
				{Source: "tool_execution", Status: "errored",
					Timestamp: "2024-06-02T11:00:00.5Z"},
			},
		},
		{
			SessionID: "dur-b", ToolName: "Bash", Category: "Bash",
			ToolUseID: "tu_b2", InputJSON: `{"command":"ls"}`,
			ResultEvents: []ToolResultEvent{
				{Source: "tool_execution", Status: "started",
					Timestamp: "2024-06-02T11:00:01Z"},
				{Source: "tool_execution", Status: "completed",
					Timestamp: "2024-06-02T11:00:02Z"},
			},
		},
	}
	insertMessages(t, d, mc)

	// Solo Edit call without event coverage inherits its turn's
	// duration (next message timestamp delta), matching the timing
	// view's AssembleTiming.
	insertSession(t, d, "dur-c", "gamma", func(s *Session) {
		s.StartedAt = new("2024-06-03T09:00:00Z")
		s.EndedAt = new("2024-06-03T09:01:00Z")
		s.MessageCount = 2
		s.Agent = "claude"
	})
	md := asstMsgAt("dur-c", 0, "[Edit: d.go]", "2024-06-03T09:00:05Z")
	md.HasToolUse = true
	md.ToolCalls = []ToolCall{
		{
			SessionID: "dur-c", ToolName: "Edit", Category: "Edit",
			ToolUseID: "tu_c1", InputJSON: `{"file_path":"d.go"}`,
		},
	}
	insertMessages(t, d, md, userMsgAt("dur-c", 1, "done",
		"2024-06-03T09:00:35Z"))
}

func TestGetAnalyticsToolsDurations(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedToolDurationFixture(t, d)

	resp, err := d.GetAnalyticsTools(ctx, baseFilter())
	require.NoError(t, err, "GetAnalyticsTools")

	byTool := make(map[string]ToolUsageAnalysis)
	for _, tool := range resp.ByTool {
		byTool[tool.ToolName] = tool
	}

	read := byTool["Read"]
	require.NotZero(t, read.CallCount, "Read row present")
	assert.Equal(t, 3, read.CallCount, "Read calls")
	// 2500ms (events) + 0 (parallel, unknown) + 500ms (events).
	assert.Equal(t, int64(3000), read.TotalDurationMs, "Read duration")

	task := byTool["Task"]
	require.NotZero(t, task.CallCount, "Task row present")
	assert.Equal(t, int64(10_000), task.TotalDurationMs, "Task duration")

	bash := byTool["Bash"]
	require.NotZero(t, bash.CallCount, "Bash row present")
	assert.Equal(t, int64(1000), bash.TotalDurationMs, "Bash duration")

	// Solo call with no event coverage inherits its turn duration.
	edit := byTool["Edit"]
	require.NotZero(t, edit.CallCount, "Edit row present")
	assert.Equal(t, int64(30_000), edit.TotalDurationMs, "Edit duration")
}

func TestGetAnalyticsToolCalls(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	seedToolDurationFixture(t, d)

	t.Run("UnknownToolIsEmpty", func(t *testing.T) {
		resp, err := d.GetAnalyticsToolCalls(
			ctx, baseFilter(), "Write", "", 0,
		)
		require.NoError(t, err, "GetAnalyticsToolCalls")
		assert.Equal(t, 0, resp.TotalCalls, "TotalCalls")
		assert.Empty(t, resp.Sessions, "Sessions")
		assert.False(t, resp.Truncated, "Truncated")
	})

	t.Run("GroupsBySessionWithDurationTotals", func(t *testing.T) {
		resp, err := d.GetAnalyticsToolCalls(
			ctx, baseFilter(), "Read", "", 0,
		)
		require.NoError(t, err, "GetAnalyticsToolCalls")
		assert.Equal(t, "Read", resp.ToolName, "ToolName")
		assert.Equal(t, 3, resp.TotalCalls, "TotalCalls")
		assert.Equal(t, int64(3000), resp.TotalDurationMs, "TotalDurationMs")
		assert.Equal(t, 2, resp.SessionCount, "SessionCount")
		assert.False(t, resp.Truncated, "Truncated")
		require.Len(t, resp.Sessions, 2, "len(Sessions)")

		// Sessions ranked by total duration desc.
		a := resp.Sessions[0]
		assert.Equal(t, "dur-a", a.SessionID, "first session")
		assert.Equal(t, "alpha", a.Project, "first project")
		assert.Equal(t, "claude", a.Agent, "first agent")
		require.NotNil(t, a.DisplayName, "display name")
		assert.Equal(t, "Alpha duration session", *a.DisplayName,
			"display name")
		assert.Equal(t, 2, a.CallCount, "dur-a calls")
		assert.Equal(t, int64(2500), a.TotalDurationMs, "dur-a duration")
		require.Len(t, a.Calls, 2, "dur-a len(calls)")
		assert.Equal(t, "a.go", a.Calls[0].InputPreview, "input preview")
		require.NotNil(t, a.Calls[0].DurationMs, "first call duration")
		assert.Equal(t, int64(2500), *a.Calls[0].DurationMs,
			"first call duration")
		assert.Nil(t, a.Calls[1].DurationMs,
			"parallel call has no measurable duration")

		b := resp.Sessions[1]
		assert.Equal(t, "dur-b", b.SessionID, "second session")
		assert.Equal(t, int64(500), b.TotalDurationMs, "dur-b duration")
		require.Len(t, b.Calls, 1, "dur-b len(calls)")
		require.NotNil(t, b.Calls[0].DurationMs, "errored call duration")
		assert.Equal(t, int64(500), *b.Calls[0].DurationMs,
			"errored call counts")
	})

	t.Run("SoloCallInheritsTurnDuration", func(t *testing.T) {
		resp, err := d.GetAnalyticsToolCalls(
			ctx, baseFilter(), "Edit", "", 0,
		)
		require.NoError(t, err, "GetAnalyticsToolCalls")
		require.Len(t, resp.Sessions, 1, "len(Sessions)")
		g := resp.Sessions[0]
		assert.Equal(t, "dur-c", g.SessionID, "session")
		assert.Equal(t, int64(30_000), g.TotalDurationMs, "group duration")
		require.Len(t, g.Calls, 1, "len(calls)")
		require.NotNil(t, g.Calls[0].DurationMs, "call duration")
		assert.Equal(t, int64(30_000), *g.Calls[0].DurationMs,
			"solo call inherits turn duration")
	})

	t.Run("CategoryFilterNarrows", func(t *testing.T) {
		resp, err := d.GetAnalyticsToolCalls(
			ctx, baseFilter(), "Read", "Bash", 0,
		)
		require.NoError(t, err, "GetAnalyticsToolCalls")
		assert.Equal(t, 0, resp.TotalCalls, "Read x Bash must not match")
	})

	t.Run("DateFilterDropsOutOfRangeCalls", func(t *testing.T) {
		f := baseFilter()
		f.To = "2024-06-01"
		resp, err := d.GetAnalyticsToolCalls(ctx, f, "Read", "", 0)
		require.NoError(t, err, "GetAnalyticsToolCalls")
		assert.Equal(t, 2, resp.TotalCalls, "TotalCalls")
		assert.Equal(t, 1, resp.SessionCount, "SessionCount")
		assert.Equal(t, int64(2500), resp.TotalDurationMs, "TotalDurationMs")
	})

	t.Run("LimitTruncatesButTotalsCoverAll", func(t *testing.T) {
		resp, err := d.GetAnalyticsToolCalls(
			ctx, baseFilter(), "Read", "", 2,
		)
		require.NoError(t, err, "GetAnalyticsToolCalls")
		assert.Equal(t, 3, resp.TotalCalls, "TotalCalls")
		assert.Equal(t, 2, resp.SessionCount, "SessionCount")
		assert.Equal(t, int64(3000), resp.TotalDurationMs, "TotalDurationMs")
		assert.True(t, resp.Truncated, "Truncated")
		var kept int
		for _, g := range resp.Sessions {
			kept += len(g.Calls)
		}
		assert.Equal(t, 2, kept, "kept calls")
	})
}

func TestBuildAnalyticsToolCallsDropsUnknownSessions(t *testing.T) {
	meta := map[string]ToolCallSessionMeta{
		"known": {Project: "alpha", Agent: "claude"},
	}
	dur := int64(100)
	rows := []ToolCallTimingRow{
		{SessionID: "known", ToolName: "Read", Category: "Read",
			DurationMs: &dur},
		{SessionID: "filtered-away", ToolName: "Read", Category: "Read"},
	}
	resp := BuildAnalyticsToolCalls("Read", "", meta, rows, 0)
	assert.Equal(t, 1, resp.TotalCalls, "TotalCalls")
	assert.Equal(t, 1, resp.SessionCount, "SessionCount")
	assert.Equal(t, int64(100), resp.TotalDurationMs, "TotalDurationMs")
}
