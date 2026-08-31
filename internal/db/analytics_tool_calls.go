package db

import "sort"

// ToolCallSessionMeta carries the display fields the per-tool drill-down
// needs for one owning session. Populated by each backend from its
// filtered sessions query.
type ToolCallSessionMeta struct {
	Project      string
	Agent        string
	StartedAt    string
	DisplayName  *string
	FirstMessage *string
}

// ToolCallTimingRow is a backend-neutral per-call row used to assemble
// the per-tool drill-down response after each store applies its native
// filters and computes the per-call duration with its own dialect.
type ToolCallTimingRow struct {
	SessionID         string
	ToolUseID         string
	ToolName          string
	Category          string
	SkillName         *string
	SubagentSessionID *string
	InputJSON         string
	Timestamp         string
	MessageOrdinal    int
	DurationMs        *int64
}

// ToolCallTiming is one call in the per-tool drill-down. DurationMs is
// null when the call has no measurable duration (parallel sibling with
// no tool_result_events coverage, or a running call).
type ToolCallTiming struct {
	ToolUseID         string  `json:"tool_use_id"`
	Category          string  `json:"category"`
	SkillName         *string `json:"skill_name,omitempty"`
	SubagentSessionID *string `json:"subagent_session_id,omitempty"`
	DurationMs        *int64  `json:"duration_ms"`
	StartedAt         string  `json:"started_at"`
	MessageOrdinal    int     `json:"message_ordinal"`
	InputPreview      string  `json:"input_preview"`
}

// ToolCallSessionGroup groups every matching call of one session.
type ToolCallSessionGroup struct {
	SessionID       string           `json:"session_id"`
	Project         string           `json:"project"`
	Agent           string           `json:"agent"`
	StartedAt       string           `json:"started_at"`
	DisplayName     *string          `json:"display_name,omitempty"`
	FirstMessage    *string          `json:"first_message,omitempty"`
	CallCount       int              `json:"call_count"`
	TotalDurationMs int64            `json:"total_duration_ms"`
	Calls           []ToolCallTiming `json:"calls"`
}

// ToolCallsResponse is the payload of the per-tool drill-down endpoint.
// Totals cover every matching call; Calls may be capped by the request
// limit, in which case Truncated is true.
type ToolCallsResponse struct {
	ToolName        string                 `json:"tool_name"`
	Category        string                 `json:"category"`
	TotalCalls      int                    `json:"total_calls"`
	TotalDurationMs int64                  `json:"total_duration_ms"`
	SessionCount    int                    `json:"session_count"`
	Truncated       bool                   `json:"truncated"`
	Sessions        []ToolCallSessionGroup `json:"sessions"`
}

// BuildAnalyticsToolCalls folds backend-neutral per-call rows into the
// public per-tool drill-down response. Only sessions present in meta
// contribute; rows arriving for unknown sessions are dropped so a
// filter change between the sessions query and the calls query cannot
// leak unfiltered sessions. Call order inside each group follows row
// order (each backend orders by session, message ordinal, call index);
// groups are ranked by total duration so the heaviest sessions lead.
func BuildAnalyticsToolCalls(
	toolName, category string,
	meta map[string]ToolCallSessionMeta,
	rows []ToolCallTimingRow,
	limit int,
) ToolCallsResponse {
	resp := ToolCallsResponse{
		ToolName: toolName,
		Category: category,
		Sessions: []ToolCallSessionGroup{},
	}
	groups := make(map[string]*ToolCallSessionGroup, len(meta))
	for _, row := range rows {
		g := groups[row.SessionID]
		if g == nil {
			m, ok := meta[row.SessionID]
			if !ok {
				continue
			}
			g = &ToolCallSessionGroup{
				SessionID:    row.SessionID,
				Project:      m.Project,
				Agent:        m.Agent,
				StartedAt:    m.StartedAt,
				DisplayName:  m.DisplayName,
				FirstMessage: m.FirstMessage,
				Calls:        []ToolCallTiming{},
			}
			groups[row.SessionID] = g
		}
		g.Calls = append(g.Calls, ToolCallTiming{
			ToolUseID:         row.ToolUseID,
			Category:          row.Category,
			SkillName:         row.SkillName,
			SubagentSessionID: row.SubagentSessionID,
			DurationMs:        row.DurationMs,
			StartedAt:         row.Timestamp,
			MessageOrdinal:    row.MessageOrdinal,
			InputPreview: makeInputPreview(
				row.Category, row.ToolName, row.InputJSON,
			),
		})
		g.CallCount++
		if row.DurationMs != nil {
			g.TotalDurationMs += *row.DurationMs
			resp.TotalDurationMs += *row.DurationMs
		}
		resp.TotalCalls++
	}

	resp.SessionCount = len(groups)
	for _, g := range groups {
		resp.Sessions = append(resp.Sessions, *g)
	}
	sort.Slice(resp.Sessions, func(i, j int) bool {
		if resp.Sessions[i].TotalDurationMs !=
			resp.Sessions[j].TotalDurationMs {
			return resp.Sessions[i].TotalDurationMs >
				resp.Sessions[j].TotalDurationMs
		}
		return resp.Sessions[i].SessionID < resp.Sessions[j].SessionID
	})

	if limit > 0 && resp.TotalCalls > limit {
		resp.Truncated = true
		remaining := limit
		for i := range resp.Sessions {
			if remaining <= 0 {
				resp.Sessions[i].Calls = []ToolCallTiming{}
				continue
			}
			if len(resp.Sessions[i].Calls) > remaining {
				resp.Sessions[i].Calls =
					resp.Sessions[i].Calls[:remaining]
				remaining = 0
			} else {
				remaining -= len(resp.Sessions[i].Calls)
			}
		}
	}
	return resp
}
