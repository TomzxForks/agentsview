package duckdb

import (
	"context"
	"testing"

	"go.kenn.io/agentsview/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDuckGetAnalyticsToolCallsAndDurations(t *testing.T) {
	ctx := context.Background()
	store := newDuckAnalyticsStore(t, []db.SessionBatchWrite{
		{
			// Child sub-agent session: 10s wall time.
			Session: func() db.Session {
				s := syncSession(
					"duck-dur-child", "alpha", "child",
					"2024-06-01T10:00:04Z", 1,
				)
				s.EndedAt = new("2024-06-01T10:00:14Z")
				return s
			}(),
			DataVersion:     1,
			ReplaceMessages: true,
		},
		{
			Session: syncSession(
				"duck-dur-a", "alpha", "parent",
				"2024-06-01T10:00:00Z", 2,
			),
			Messages: []db.Message{
				syncMessage("duck-dur-a", 0, "assistant",
					"reads", "2024-06-01T10:00:01Z",
					db.ToolCall{
						ToolName: "Read", Category: "Read",
						ToolUseID: "tu_a1",
						InputJSON: `{"file_path":"a.go"}`,
						ResultEvents: []db.ToolResultEvent{
							{Source: "tool_execution",
								Status:    "started",
								Timestamp: "2024-06-01T10:00:01Z"},
							{Source: "tool_execution",
								Status:    "completed",
								Timestamp: "2024-06-01T10:00:03.5Z"},
						},
					},
					db.ToolCall{
						ToolName: "Read", Category: "Read",
						ToolUseID: "tu_a2",
						InputJSON: `{"file_path":"b.go"}`,
					},
				),
				syncMessage("duck-dur-a", 1, "assistant",
					"spawn", "2024-06-01T10:00:04Z",
					db.ToolCall{
						ToolName: "Task", Category: "Task",
						ToolUseID:         "tu_sub",
						SubagentSessionID: "duck-dur-child",
					},
				),
			},
			DataVersion:     1,
			ReplaceMessages: true,
		},
		{
			Session: syncSession(
				"duck-dur-b", "beta", "second",
				"2024-06-02T11:00:00Z", 1,
			),
			Messages: []db.Message{
				syncMessage("duck-dur-b", 0, "assistant",
					"read", "2024-06-02T11:00:00Z",
					db.ToolCall{
						ToolName: "Read", Category: "Read",
						ToolUseID: "tu_b1",
						InputJSON: `{"file_path":"c.go"}`,
						ResultEvents: []db.ToolResultEvent{
							{Source: "tool_execution",
								Status:    "started",
								Timestamp: "2024-06-02T11:00:00Z"},
							{Source: "tool_execution",
								Status:    "errored",
								Timestamp: "2024-06-02T11:00:00.5Z"},
						},
					},
				),
			},
			DataVersion:     1,
			ReplaceMessages: true,
		},
	})

	t.Run("ToolsAggregationSumsDurations", func(t *testing.T) {
		resp, err := store.GetAnalyticsTools(ctx, db.AnalyticsFilter{
			From: "2024-06-01", To: "2024-06-03", Timezone: "UTC",
		})
		require.NoError(t, err, "GetAnalyticsTools")
		byTool := make(map[string]db.ToolUsageAnalysis)
		for _, tool := range resp.ByTool {
			byTool[tool.ToolName] = tool
		}
		require.NotZero(t, byTool["Read"].CallCount, "Read present")
		assert.Equal(t, int64(3000), byTool["Read"].TotalDurationMs,
			"Read duration")
		assert.Equal(t, int64(10_000), byTool["Task"].TotalDurationMs,
			"Task duration")
	})

	t.Run("ToolCallsGroupsBySession", func(t *testing.T) {
		resp, err := store.GetAnalyticsToolCalls(
			ctx,
			db.AnalyticsFilter{
				From: "2024-06-01", To: "2024-06-03", Timezone: "UTC",
			},
			"Read", "", 0,
		)
		require.NoError(t, err, "GetAnalyticsToolCalls")
		assert.Equal(t, 3, resp.TotalCalls, "TotalCalls")
		assert.Equal(t, int64(3000), resp.TotalDurationMs,
			"TotalDurationMs")
		assert.Equal(t, 2, resp.SessionCount, "SessionCount")
		require.Len(t, resp.Sessions, 2, "len(Sessions)")

		a := resp.Sessions[0]
		assert.Equal(t, "duck-dur-a", a.SessionID, "first session")
		assert.Equal(t, "alpha", a.Project, "first project")
		assert.Equal(t, 2, a.CallCount, "dur-a calls")
		assert.Equal(t, int64(2500), a.TotalDurationMs, "dur-a duration")
		require.Len(t, a.Calls, 2, "dur-a len(calls)")
		assert.Equal(t, "a.go", a.Calls[0].InputPreview, "input preview")
		require.NotNil(t, a.Calls[0].DurationMs, "first call duration")
		assert.Equal(t, int64(2500), *a.Calls[0].DurationMs,
			"first call duration")
		assert.Nil(t, a.Calls[1].DurationMs, "parallel call duration")

		b := resp.Sessions[1]
		assert.Equal(t, "duck-dur-b", b.SessionID, "second session")
		require.Len(t, b.Calls, 1, "dur-b len(calls)")
		require.NotNil(t, b.Calls[0].DurationMs, "errored call duration")
		assert.Equal(t, int64(500), *b.Calls[0].DurationMs,
			"errored call duration")
	})

	t.Run("LimitTruncatesButTotalsCoverAll", func(t *testing.T) {
		resp, err := store.GetAnalyticsToolCalls(
			ctx,
			db.AnalyticsFilter{
				From: "2024-06-01", To: "2024-06-03", Timezone: "UTC",
			},
			"Read", "", 2,
		)
		require.NoError(t, err, "GetAnalyticsToolCalls")
		assert.Equal(t, 3, resp.TotalCalls, "TotalCalls")
		assert.True(t, resp.Truncated, "Truncated")
		var kept int
		for _, g := range resp.Sessions {
			kept += len(g.Calls)
		}
		assert.Equal(t, 2, kept, "kept calls")
	})
}
