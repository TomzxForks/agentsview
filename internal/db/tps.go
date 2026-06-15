package db

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/tidwall/gjson"
)

// TPSPercentiles holds p50/p75/p90/p95/pMax values.
type TPSPercentiles struct {
	P50  float64 `json:"p50"`
	P75  float64 `json:"p75"`
	P90  float64 `json:"p90"`
	P95  float64 `json:"p95"`
	PMax float64 `json:"p_max"`
}

// TPSOverview holds aggregate TPS metrics across all turns.
type TPSOverview struct {
	TotalSessions     int            `json:"total_sessions"`
	TotalTurns        int            `json:"total_turns"`
	AverageTPS        float64        `json:"average_tps"`
	AverageITPS       float64        `json:"average_itps"`
	AverageOTPS       float64        `json:"average_otps"`
	TPSPercentiles    TPSPercentiles `json:"tps_percentiles"`
	ITPSPercentiles   TPSPercentiles `json:"itps_percentiles"`
	OTPSPercentiles   TPSPercentiles `json:"otps_percentiles"`
	TotalTokens       int64          `json:"total_tokens"`
	TotalInputTokens  int64          `json:"total_input_tokens"`
	TotalOutputTokens int64          `json:"total_output_tokens"`
}

// TPSModelStat holds per-model TPS metrics.
type TPSModelStat struct {
	Model             string         `json:"model"`
	AverageTPS        float64        `json:"average_tps"`
	AverageITPS       float64        `json:"average_itps"`
	AverageOTPS       float64        `json:"average_otps"`
	TurnCount         int            `json:"turn_count"`
	TotalTokens       int64          `json:"total_tokens"`
	TotalInputTokens  int64          `json:"total_input_tokens"`
	TotalOutputTokens int64          `json:"total_output_tokens"`
	TotalDuration     float64        `json:"total_duration_seconds"`
	TPSPercentiles    TPSPercentiles `json:"tps_percentiles"`
	ITPSPercentiles   TPSPercentiles `json:"itps_percentiles"`
	OTPSPercentiles   TPSPercentiles `json:"otps_percentiles"`
}

// TPSSessionSummary holds per-session TPS metrics.
type TPSSessionSummary struct {
	SessionID    string   `json:"session_id"`
	Timestamp    string   `json:"timestamp"`
	TurnCount    int      `json:"turn_count"`
	TotalTokens  int64    `json:"total_tokens"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	AverageTPS   float64  `json:"average_tps"`
	AverageITPS  float64  `json:"average_itps"`
	AverageOTPS  float64  `json:"average_otps"`
	Models       []string `json:"models"`
}

// TPSTurn holds a single computed TPS data point (one conversation turn).
type TPSTurn struct {
	SessionID       string  `json:"session_id"`
	Timestamp       string  `json:"timestamp"`
	TPS             float64 `json:"tps"`
	ITPS            float64 `json:"itps"`
	OTPS            float64 `json:"otps"`
	TotalTokens     int64   `json:"total_tokens"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	DurationSeconds float64 `json:"duration_seconds"`
	Model           string  `json:"model"`
}

// TPSResponse is the payload for GET /api/v1/analytics/tps.
type TPSResponse struct {
	Overview TPSOverview         `json:"overview"`
	ByModel  []TPSModelStat      `json:"by_model"`
	Sessions []TPSSessionSummary `json:"sessions"`
	Turns    []TPSTurn           `json:"turns"`
}

// tpsPercentileSet computes p50/p75/p90/p95/pMax from a
// pre-sorted float64 slice using the nearest-rank method,
// matching tps-viewer's percentile semantics.
func tpsPercentileSet(sorted []float64) TPSPercentiles {
	n := len(sorted)
	if n == 0 {
		return TPSPercentiles{}
	}
	return TPSPercentiles{
		P50:  percentileFloat(sorted, 0.50),
		P75:  percentileFloat(sorted, 0.75),
		P90:  percentileFloat(sorted, 0.90),
		P95:  percentileFloat(sorted, 0.95),
		PMax: sorted[n-1],
	}
}

// AggregateTPS computes the overall, per-model, and per-session
// summaries from a flat slice of turn data points. This is the
// shared aggregation function called by both the SQLite and PG
// backends, mirroring the AggregateSignals pattern.
func AggregateTPS(turns []TPSTurn) TPSResponse {
	resp := TPSResponse{
		ByModel:  []TPSModelStat{},
		Sessions: []TPSSessionSummary{},
		Turns:    turns,
	}

	if len(turns) == 0 {
		return resp
	}

	// Overall accumulators.
	var allTPS, allITPS, allOTPS []float64
	var totalTokens, totalInput, totalOutput int64
	sessionsSet := make(map[string]bool)

	// Per-model accumulators.
	type modelAccum struct {
		tps, itps, otps         []float64
		totalTokens             int64
		totalInput, totalOutput int64
		totalDuration           float64
	}
	modelMap := make(map[string]*modelAccum)

	// Per-session accumulators.
	type sessionAccum struct {
		tps, itps, otps []float64
		totalTokens     int64
		totalInput      int64
		totalOutput     int64
		timestamp       string
		models          map[string]bool
	}
	sessionMap := make(map[string]*sessionAccum)

	for _, t := range turns {
		sessionsSet[t.SessionID] = true

		allTPS = append(allTPS, t.TPS)
		allITPS = append(allITPS, t.ITPS)
		allOTPS = append(allOTPS, t.OTPS)
		totalTokens += t.TotalTokens
		totalInput += t.InputTokens
		totalOutput += t.OutputTokens

		// Model accumulation.
		ma := modelMap[t.Model]
		if ma == nil {
			ma = &modelAccum{}
			modelMap[t.Model] = ma
		}
		ma.tps = append(ma.tps, t.TPS)
		ma.itps = append(ma.itps, t.ITPS)
		ma.otps = append(ma.otps, t.OTPS)
		ma.totalTokens += t.TotalTokens
		ma.totalInput += t.InputTokens
		ma.totalOutput += t.OutputTokens
		ma.totalDuration += t.DurationSeconds

		// Session accumulation.
		sa := sessionMap[t.SessionID]
		if sa == nil {
			sa = &sessionAccum{
				timestamp: t.Timestamp,
				models:    make(map[string]bool),
			}
			sessionMap[t.SessionID] = sa
		}
		sa.tps = append(sa.tps, t.TPS)
		sa.itps = append(sa.itps, t.ITPS)
		sa.otps = append(sa.otps, t.OTPS)
		sa.totalTokens += t.TotalTokens
		sa.totalInput += t.InputTokens
		sa.totalOutput += t.OutputTokens
		sa.models[t.Model] = true
	}

	// Overview.
	sort.Float64s(allTPS)
	sort.Float64s(allITPS)
	sort.Float64s(allOTPS)
	n := len(turns)
	resp.Overview = TPSOverview{
		TotalSessions:     len(sessionsSet),
		TotalTurns:        n,
		AverageTPS:        meanFloats(allTPS),
		AverageITPS:       meanFloats(allITPS),
		AverageOTPS:       meanFloats(allOTPS),
		TPSPercentiles:    tpsPercentileSet(allTPS),
		ITPSPercentiles:   tpsPercentileSet(allITPS),
		OTPSPercentiles:   tpsPercentileSet(allOTPS),
		TotalTokens:       totalTokens,
		TotalInputTokens:  totalInput,
		TotalOutputTokens: totalOutput,
	}

	// By-model, sorted by total tokens descending (matches tps-viewer).
	for model, ma := range modelMap {
		sort.Float64s(ma.tps)
		sort.Float64s(ma.itps)
		sort.Float64s(ma.otps)
		mc := len(ma.tps)
		resp.ByModel = append(resp.ByModel, TPSModelStat{
			Model:             model,
			AverageTPS:        meanFloats(ma.tps),
			AverageITPS:       meanFloats(ma.itps),
			AverageOTPS:       meanFloats(ma.otps),
			TurnCount:         mc,
			TotalTokens:       ma.totalTokens,
			TotalInputTokens:  ma.totalInput,
			TotalOutputTokens: ma.totalOutput,
			TotalDuration:     roundTo(ma.totalDuration, 2),
			TPSPercentiles:    tpsPercentileSet(ma.tps),
			ITPSPercentiles:   tpsPercentileSet(ma.itps),
			OTPSPercentiles:   tpsPercentileSet(ma.otps),
		})
	}
	sort.Slice(resp.ByModel, func(i, j int) bool {
		return resp.ByModel[i].TotalTokens > resp.ByModel[j].TotalTokens
	})

	// By-session, sorted by timestamp descending (matches tps-viewer).
	for sid, sa := range sessionMap {
		sort.Float64s(sa.tps)
		sort.Float64s(sa.itps)
		sort.Float64s(sa.otps)
		models := make([]string, 0, len(sa.models))
		for m := range sa.models {
			models = append(models, m)
		}
		sort.Strings(models)
		resp.Sessions = append(resp.Sessions, TPSSessionSummary{
			SessionID:    sid,
			Timestamp:    sa.timestamp,
			TurnCount:    len(sa.tps),
			TotalTokens:  sa.totalTokens,
			InputTokens:  sa.totalInput,
			OutputTokens: sa.totalOutput,
			AverageTPS:   meanFloats(sa.tps),
			AverageITPS:  meanFloats(sa.itps),
			AverageOTPS:  meanFloats(sa.otps),
			Models:       models,
		})
	}
	sort.Slice(resp.Sessions, func(i, j int) bool {
		return resp.Sessions[i].Timestamp > resp.Sessions[j].Timestamp
	})

	return resp
}

func roundTo(v float64, decimals int) float64 {
	mult := 1.0
	for i := 0; i < decimals; i++ {
		mult *= 10
	}
	return float64(int64(v*mult+0.5)) / mult
}

// ParsedTokenUsage holds the token counts extracted from a
// token_usage JSON blob.
type ParsedTokenUsage struct {
	Input  int
	Output int
}

// ParseTokenUsage extracts input_tokens and output_tokens from
// a token_usage JSON string via gjson. Returns zeros when the
// JSON is empty or keys are absent. Shared by the SQLite and PG
// TPS implementations so both backends parse identically.
func ParseTokenUsage(tokenUsage string) ParsedTokenUsage {
	if tokenUsage == "" {
		return ParsedTokenUsage{}
	}
	parsed := gjson.Parse(tokenUsage)
	return ParsedTokenUsage{
		Input:  int(parsed.Get("input_tokens").Int()),
		Output: int(parsed.Get("output_tokens").Int()),
	}
}

// tpsRawMsg holds per-message data needed for TPS turn computation.
type tpsRawMsg struct {
	role        string
	ts          time.Time
	valid       bool
	model       string
	tokenUsage  string
	inputTokens int
	outputTokns int
	hasToolUse  bool
}

// computeTPSTurns groups messages into conversation turns and
// computes per-turn TPS data points, mirroring tps-viewer's
// calculateTurnTPS. A turn = user message followed by one or
// more assistant messages. Duration = user timestamp to last
// assistant timestamp. Turns with zero/negative duration are
// rejected. The turn's model = first model seen in the
// assistant messages.
func computeTPSTurns(sid string, msgs []tpsRawMsg) []TPSTurn {
	var turns []TPSTurn

	type currentTurn struct {
		userTS     time.Time
		valid      bool
		assistants []tpsRawMsg
	}
	cur := currentTurn{}

	flush := func() {
		if !cur.valid || len(cur.assistants) == 0 {
			cur = currentTurn{}
			return
		}
		t := computeOneTurn(sid, cur.userTS, cur.assistants)
		if t != nil {
			turns = append(turns, *t)
		}
		cur = currentTurn{}
	}

	for _, m := range msgs {
		if m.role == "user" {
			flush()
			if m.valid {
				cur.userTS = m.ts
				cur.valid = true
			}
		} else if m.role == "assistant" && cur.valid {
			cur.assistants = append(cur.assistants, m)
		}
	}
	flush()

	return turns
}

// computeOneTurn computes a single turn's TPS from a user
// timestamp and the following assistant messages. Returns nil
// for zero/negative duration turns.
func computeOneTurn(
	sid string, userTS time.Time, assistants []tpsRawMsg,
) *TPSTurn {
	var totalTokens, inputTokens, outputTokens int64
	lastTS := userTS
	model := ""
	modelFound := false

	for _, a := range assistants {
		// Skip tool-call messages: their output_tokens are tool
		// call parameters, not model-generated response text.
		// They still participate in timestamp/model tracking.
		if !a.hasToolUse {
			in := int64(a.inputTokens)
			out := int64(a.outputTokns)
			totalTokens += in + out
			inputTokens += in
			outputTokens += out
		}
		if a.ts.After(lastTS) {
			lastTS = a.ts
		}
		if !modelFound && a.model != "" {
			model = a.model
			modelFound = true
		}
	}

	duration := lastTS.Sub(userTS).Seconds()
	if duration <= 0 {
		return nil
	}
	if !modelFound {
		model = "unknown"
	}

	return &TPSTurn{
		SessionID:       sid,
		Timestamp:       userTS.Format(time.RFC3339),
		TPS:             float64(totalTokens) / duration,
		ITPS:            float64(inputTokens) / duration,
		OTPS:            float64(outputTokens) / duration,
		TotalTokens:     totalTokens,
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		DurationSeconds: roundTo(duration, 3),
		Model:           model,
	}
}

// GetAnalyticsTPS computes the TPS dashboard response for the
// given filter. It fetches filtered sessions, then per-session
// messages (including token_usage JSON and model), groups them
// into conversation turns, and aggregates via AggregateTPS.
func (db *DB) GetAnalyticsTPS(
	ctx context.Context, f AnalyticsFilter,
) (TPSResponse, error) {
	loc := f.location()
	dateCol := "COALESCE(NULLIF(started_at, ''), created_at)"
	where, args := f.buildWhere(dateCol)

	var timeIDs map[string]bool
	if f.HasTimeFilter() {
		var err error
		timeIDs, err = db.filteredSessionIDs(ctx, f)
		if err != nil {
			return TPSResponse{}, err
		}
	}

	// Phase 1: Get filtered session IDs.
	sessQuery := `SELECT id, ` + dateCol + `
		FROM sessions WHERE ` + where

	sessRows, err := db.getReader().QueryContext(ctx, sessQuery, args...)
	if err != nil {
		return TPSResponse{}, fmt.Errorf(
			"querying TPS sessions: %w", err,
		)
	}
	var sessionIDs []string
	for sessRows.Next() {
		var id, ts string
		if err := sessRows.Scan(&id, &ts); err != nil {
			sessRows.Close()
			return TPSResponse{}, fmt.Errorf(
				"scanning TPS session: %w", err,
			)
		}
		date := localDate(ts, loc)
		if !inDateRange(date, f.From, f.To) {
			continue
		}
		if timeIDs != nil && !timeIDs[id] {
			continue
		}
		sessionIDs = append(sessionIDs, id)
	}
	if err := sessRows.Err(); err != nil {
		sessRows.Close()
		return TPSResponse{}, fmt.Errorf(
			"iterating TPS sessions: %w", err,
		)
	}
	sessRows.Close()

	if len(sessionIDs) == 0 {
		return TPSResponse{
			ByModel:  []TPSModelStat{},
			Sessions: []TPSSessionSummary{},
			Turns:    []TPSTurn{},
		}, nil
	}

	// Phase 2: Fetch messages per session (chunked).
	sessionMsgs := make(map[string][]tpsRawMsg)
	err = queryChunked(sessionIDs,
		func(chunk []string) error {
			return db.queryTPSMessages(
				ctx, chunk, loc, sessionMsgs,
			)
		})
	if err != nil {
		return TPSResponse{}, err
	}

	// Phase 3: Compute turns per session.
	var allTurns []TPSTurn
	for _, sid := range sessionIDs {
		msgs := sessionMsgs[sid]
		if len(msgs) < 2 {
			continue
		}
		turns := computeTPSTurns(sid, msgs)
		allTurns = append(allTurns, turns...)
	}

	// Phase 4: Aggregate.
	return AggregateTPS(allTurns), nil
}

// queryTPSMessages fetches eligible messages (with token_usage
// and model) for a chunk of session IDs, ordered by ordinal.
// Only user and assistant messages with valid timestamps are
// included. Token counts are extracted from the token_usage
// JSON blob via gjson.
func (db *DB) queryTPSMessages(
	ctx context.Context,
	chunk []string,
	loc *time.Location,
	sessionMsgs map[string][]tpsRawMsg,
) error {
	ph, args := inPlaceholders(chunk)
	q := `SELECT session_id, ordinal, role, timestamp,
		model, token_usage, has_tool_use
		FROM messages
		WHERE session_id IN ` + ph + `
			AND role IN ('user', 'assistant')
			AND timestamp != ''
		ORDER BY session_id, ordinal`

	rows, err := db.getReader().QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("querying TPS messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sid, role, model, tokenUsage string
		var ordinal, hasToolUse int
		var tsStr string
		if err := rows.Scan(
			&sid, &ordinal, &role, &tsStr, &model,
			&tokenUsage, &hasToolUse,
		); err != nil {
			return fmt.Errorf("scanning TPS msg: %w", err)
		}
		ts, ok := localTime(tsStr, loc)
		if !ok {
			continue
		}
		msg := tpsRawMsg{
			role:       role,
			ts:         ts,
			valid:      true,
			model:      model,
			tokenUsage: tokenUsage,
			hasToolUse: hasToolUse == 1,
		}
		// Extract token counts from token_usage JSON for
		// assistant messages that are NOT tool calls. Tool-call
		// messages are excluded from token totals because their
		// output_tokens are tool parameters, not model response.
		if role == "assistant" && !msg.hasToolUse &&
			tokenUsage != "" &&
			model != "" && model != "<synthetic>" {
			pt := ParseTokenUsage(tokenUsage)
			msg.inputTokens = pt.Input
			msg.outputTokns = pt.Output
		}
		sessionMsgs[sid] = append(sessionMsgs[sid], msg)
	}
	return rows.Err()
}
