package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustParse(t *testing.T, ts string) time.Time {
	t.Helper()
	out, err := time.Parse(time.RFC3339, ts)
	require.NoError(t, err)
	return out
}

func TestAggregateTPS_Empty(t *testing.T) {
	resp := AggregateTPS(nil)
	require.NotNil(t, resp)
	assert.Empty(t, resp.ByModel)
	assert.Empty(t, resp.Sessions)
	assert.Empty(t, resp.Turns)
	assert.Equal(t, 0, resp.Overview.TotalTurns)
	assert.Equal(t, 0, resp.Overview.TotalSessions)
}

func TestAggregateTPS_SingleModel(t *testing.T) {
	turns := []TPSTurn{
		{SessionID: "s1", Timestamp: "2025-01-01T10:00:00Z",
			TPS: 50, ITPS: 20, OTPS: 30,
			TotalTokens: 500, InputTokens: 200, OutputTokens: 300,
			DurationSeconds: 10, Model: "claude-opus"},
		{SessionID: "s1", Timestamp: "2025-01-01T11:00:00Z",
			TPS: 100, ITPS: 40, OTPS: 60,
			TotalTokens: 1000, InputTokens: 400, OutputTokens: 600,
			DurationSeconds: 10, Model: "claude-opus"},
	}
	resp := AggregateTPS(turns)

	assert.Equal(t, 1, resp.Overview.TotalSessions)
	assert.Equal(t, 2, resp.Overview.TotalTurns)
	assert.InDelta(t, 75.0, resp.Overview.AverageTPS, 0.01)
	assert.InDelta(t, 30.0, resp.Overview.AverageITPS, 0.01)
	assert.InDelta(t, 45.0, resp.Overview.AverageOTPS, 0.01)
	assert.Equal(t, int64(1500), resp.Overview.TotalTokens)
	assert.Equal(t, int64(600), resp.Overview.TotalInputTokens)
	assert.Equal(t, int64(900), resp.Overview.TotalOutputTokens)

	require.Len(t, resp.ByModel, 1)
	m := resp.ByModel[0]
	assert.Equal(t, "claude-opus", m.Model)
	assert.Equal(t, 2, m.TurnCount)
	assert.InDelta(t, 75.0, m.AverageTPS, 0.01)
	assert.Equal(t, int64(1500), m.TotalTokens)

	require.Len(t, resp.Sessions, 1)
	s := resp.Sessions[0]
	assert.Equal(t, "s1", s.SessionID)
	assert.Equal(t, 2, s.TurnCount)
}

func TestAggregateTPS_MultiModel(t *testing.T) {
	turns := []TPSTurn{
		{SessionID: "s1", Timestamp: "2025-01-01T10:00:00Z",
			TPS: 50, ITPS: 20, OTPS: 30,
			TotalTokens: 500, InputTokens: 200, OutputTokens: 300,
			DurationSeconds: 10, Model: "claude-opus"},
		{SessionID: "s2", Timestamp: "2025-01-01T11:00:00Z",
			TPS: 100, ITPS: 40, OTPS: 60,
			TotalTokens: 2000, InputTokens: 800, OutputTokens: 1200,
			DurationSeconds: 10, Model: "claude-sonnet"},
	}
	resp := AggregateTPS(turns)

	assert.Equal(t, 2, resp.Overview.TotalSessions)
	assert.Equal(t, 2, resp.Overview.TotalTurns)
	assert.Equal(t, int64(2500), resp.Overview.TotalTokens)

	require.Len(t, resp.ByModel, 2)
	assert.Equal(t, "claude-sonnet", resp.ByModel[0].Model)
	assert.Equal(t, int64(2000), resp.ByModel[0].TotalTokens)
	assert.Equal(t, "claude-opus", resp.ByModel[1].Model)
	assert.Equal(t, int64(500), resp.ByModel[1].TotalTokens)
}

func TestAggregateTPS_Percentiles(t *testing.T) {
	turns := []TPSTurn{
		{SessionID: "s1", TPS: 10, ITPS: 4, OTPS: 6,
			TotalTokens: 100, InputTokens: 40, OutputTokens: 60,
			DurationSeconds: 10, Model: "m1", Timestamp: "2025-01-01T10:00:00Z"},
		{SessionID: "s1", TPS: 20, ITPS: 8, OTPS: 12,
			TotalTokens: 200, InputTokens: 80, OutputTokens: 120,
			DurationSeconds: 10, Model: "m1", Timestamp: "2025-01-01T11:00:00Z"},
		{SessionID: "s1", TPS: 30, ITPS: 12, OTPS: 18,
			TotalTokens: 300, InputTokens: 120, OutputTokens: 180,
			DurationSeconds: 10, Model: "m1", Timestamp: "2025-01-01T12:00:00Z"},
		{SessionID: "s1", TPS: 40, ITPS: 16, OTPS: 24,
			TotalTokens: 400, InputTokens: 160, OutputTokens: 240,
			DurationSeconds: 10, Model: "m1", Timestamp: "2025-01-01T13:00:00Z"},
	}
	resp := AggregateTPS(turns)

	p := resp.Overview.TPSPercentiles
	// percentileFloat uses int(n * pct) truncation: for n=4,
	// p50→idx2=30, p75→idx3=40, p90→idx3=40, p95→idx3=40.
	assert.InDelta(t, 30.0, p.P50, 0.01)
	assert.InDelta(t, 40.0, p.P75, 0.01)
	assert.InDelta(t, 40.0, p.P90, 0.01)
	assert.InDelta(t, 40.0, p.P95, 0.01)
	assert.InDelta(t, 40.0, p.PMax, 0.01)
}

func TestComputeTPSTurns_BasicTurn(t *testing.T) {
	base := mustParse(t, "2025-01-01T10:00:00Z")
	msgs := []tpsRawMsg{
		{role: "user", ts: base, valid: true},
		{role: "assistant", ts: base.Add(10 * time.Second),
			valid: true, model: "claude",
			inputTokens: 100, outputTokns: 200},
	}
	turns := computeTPSTurns("s1", msgs)
	require.Len(t, turns, 1)

	t1 := turns[0]
	assert.Equal(t, "s1", t1.SessionID)
	assert.Equal(t, "claude", t1.Model)
	assert.Equal(t, int64(300), t1.TotalTokens)
	assert.Equal(t, int64(100), t1.InputTokens)
	assert.Equal(t, int64(200), t1.OutputTokens)
	assert.InDelta(t, 30.0, t1.TPS, 0.01)
	assert.InDelta(t, 10.0, t1.ITPS, 0.01)
	assert.InDelta(t, 20.0, t1.OTPS, 0.01)
}

func TestComputeTPSTurns_MultipleAssistants(t *testing.T) {
	base := mustParse(t, "2025-01-01T10:00:00Z")
	msgs := []tpsRawMsg{
		{role: "user", ts: base, valid: true},
		{role: "assistant", ts: base.Add(5 * time.Second),
			valid: true, model: "claude",
			inputTokens: 50, outputTokns: 100},
		{role: "assistant", ts: base.Add(10 * time.Second),
			valid: true, model: "claude",
			inputTokens: 50, outputTokns: 100},
	}
	turns := computeTPSTurns("s1", msgs)
	require.Len(t, turns, 1)

	t1 := turns[0]
	assert.Equal(t, int64(300), t1.TotalTokens)
	assert.InDelta(t, 30.0, t1.TPS, 0.01)
}

func TestComputeTPSTurns_RejectsZeroDuration(t *testing.T) {
	base := mustParse(t, "2025-01-01T10:00:00Z")
	msgs := []tpsRawMsg{
		{role: "user", ts: base, valid: true},
		{role: "assistant", ts: base,
			valid: true, model: "claude",
			inputTokens: 100, outputTokns: 200},
	}
	turns := computeTPSTurns("s1", msgs)
	assert.Empty(t, turns)
}

func TestComputeTPSTurns_MultiTurn(t *testing.T) {
	base := mustParse(t, "2025-01-01T10:00:00Z")
	msgs := []tpsRawMsg{
		{role: "user", ts: base, valid: true},
		{role: "assistant", ts: base.Add(10 * time.Second),
			valid: true, model: "claude",
			inputTokens: 100, outputTokns: 200},
		{role: "user", ts: base.Add(20 * time.Second), valid: true},
		{role: "assistant", ts: base.Add(30 * time.Second),
			valid: true, model: "claude",
			inputTokens: 200, outputTokns: 400},
	}
	turns := computeTPSTurns("s1", msgs)
	require.Len(t, turns, 2)
	assert.Equal(t, int64(300), turns[0].TotalTokens)
	assert.Equal(t, int64(600), turns[1].TotalTokens)
}

func TestComputeTPSTurns_FirstModelAttribution(t *testing.T) {
	base := mustParse(t, "2025-01-01T10:00:00Z")
	msgs := []tpsRawMsg{
		{role: "user", ts: base, valid: true},
		{role: "assistant", ts: base.Add(5 * time.Second),
			valid: true, model: "claude-opus",
			inputTokens: 50, outputTokns: 100},
		{role: "assistant", ts: base.Add(10 * time.Second),
			valid: true, model: "claude-sonnet",
			inputTokens: 50, outputTokns: 100},
	}
	turns := computeTPSTurns("s1", msgs)
	require.Len(t, turns, 1)
	assert.Equal(t, "claude-opus", turns[0].Model)
}

func TestComputeTPSTurns_NoTokenUsage(t *testing.T) {
	base := mustParse(t, "2025-01-01T10:00:00Z")
	msgs := []tpsRawMsg{
		{role: "user", ts: base, valid: true},
		{role: "assistant", ts: base.Add(10 * time.Second),
			valid: true, model: "claude"},
	}
	turns := computeTPSTurns("s1", msgs)
	require.Len(t, turns, 1)
	assert.Equal(t, int64(0), turns[0].TotalTokens)
	assert.InDelta(t, 0.0, turns[0].TPS, 0.01)
}

func TestComputeTPSTurns_ExcludesToolUseMessages(t *testing.T) {
	base := mustParse(t, "2025-01-01T10:00:00Z")
	msgs := []tpsRawMsg{
		{role: "user", ts: base, valid: true},
		// Tool-call message: tokens should be excluded
		{role: "assistant", ts: base.Add(3 * time.Second),
			valid: true, model: "claude",
			inputTokens: 5000, outputTokns: 800,
			hasToolUse: true},
		// Tool-call message: tokens should be excluded
		{role: "assistant", ts: base.Add(6 * time.Second),
			valid: true, model: "claude",
			inputTokens: 10000, outputTokns: 500,
			hasToolUse: true},
		// Text response: tokens should be counted
		{role: "assistant", ts: base.Add(10 * time.Second),
			valid: true, model: "claude",
			inputTokens: 100, outputTokns: 200},
	}
	turns := computeTPSTurns("s1", msgs)
	require.Len(t, turns, 1)

	t1 := turns[0]
	// Only the text response's tokens counted
	assert.Equal(t, int64(300), t1.TotalTokens)
	assert.Equal(t, int64(100), t1.InputTokens)
	assert.Equal(t, int64(200), t1.OutputTokens)
	// Duration spans all messages including tool calls
	assert.InDelta(t, 30.0, t1.TPS, 0.01)
	assert.InDelta(t, 20.0, t1.OTPS, 0.01)
}

func TestParseTokenUsage(t *testing.T) {
	pt := ParseTokenUsage(
		`{"input_tokens":150,"output_tokens":75}`)
	assert.Equal(t, 150, pt.Input)
	assert.Equal(t, 75, pt.Output)

	pt = ParseTokenUsage("")
	assert.Equal(t, 0, pt.Input)
	assert.Equal(t, 0, pt.Output)

	pt = ParseTokenUsage(`{"input_tokens":200}`)
	assert.Equal(t, 200, pt.Input)
	assert.Equal(t, 0, pt.Output)
}
