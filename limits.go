package agent

import "time"

// Limits bound runtime work and retained JSON bytes. MaxTokens is a post-hoc
// input+output accounting ceiling, not a hard billing or preflight token bound.
// MaxOutputTokens is also sent to the provider for each model request.
type Limits struct {
	MaxInputBytes       int
	MaxRequestBytes     int
	MaxModelOutputBytes int
	MaxToolOutputBytes  int
	MaxModelCalls       int
	MaxToolCalls        int
	MaxRepairs          int
	MaxTokens           int64
	MaxOutputTokens     int64
	RunTimeout          time.Duration
	ModelTimeout        time.Duration
	ToolTimeout         time.Duration
}

func DefaultLimits() Limits {
	return Limits{
		MaxInputBytes:       32 << 10,
		MaxRequestBytes:     256 << 10,
		MaxModelOutputBytes: 64 << 10,
		MaxToolOutputBytes:  32 << 10,
		MaxModelCalls:       8,
		MaxToolCalls:        16,
		MaxRepairs:          2,
		MaxTokens:           32000,
		MaxOutputTokens:     2048,
		RunTimeout:          2 * time.Minute,
		ModelTimeout:        30 * time.Second,
		ToolTimeout:         10 * time.Second,
	}
}

func (l Limits) valid() bool {
	return l.MaxInputBytes > 0 && l.MaxRequestBytes > 0 &&
		l.MaxModelOutputBytes > 0 && l.MaxToolOutputBytes > 0 &&
		l.MaxModelCalls > 0 && l.MaxToolCalls > 0 && l.MaxRepairs >= 0 &&
		l.MaxTokens > 0 && l.MaxOutputTokens > 0 &&
		l.RunTimeout > 0 && l.ModelTimeout > 0 && l.ToolTimeout > 0
}

// Restriction is absent at its zero value. OnlyTools() explicitly denies all.
// Unknown or out-of-agent IDs are errors rather than silently ignored entries.
type Restriction struct {
	set bool
	ids []string
}

func OnlyTools(ids ...string) Restriction {
	return Restriction{set: true, ids: append([]string(nil), ids...)}
}
