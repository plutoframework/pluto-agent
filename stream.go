package agent

import (
	"context"
	"sync"
)

type EventKind string

const (
	EventModelStart EventKind = "model_start"
	EventPartial    EventKind = "partial_unvalidated"
	EventToolStart  EventKind = "tool_start"
	EventToolEnd    EventKind = "tool_end"
	EventRepair     EventKind = "repair"
	EventFinal      EventKind = "final_validated"
)

type Event[O any] struct {
	Kind    EventKind
	Partial string
	Tool    string

	// Output is set only on EventFinal after all validation succeeds.
	Output *O
}

// RunStream is synchronous: emit provides backpressure and must honor ctx.
// Returning an error stops the run. Partial events may belong to an attempt
// that fails validation; only EventFinal represents accepted output.
func (a *Agent[D, O]) RunStream(
	ctx context.Context,
	run Run[D],
	emit func(Event[O]) error,
) (Result[O], error) {
	if a == nil || emit == nil {
		return Result[O]{Status: StatusFailed}, ErrConfig
	}

	_, ok := a.config.Provider.(StreamingProvider)
	if !ok || !a.config.Provider.Capabilities().Streaming {
		return Result[O]{Status: StatusFailed}, ErrCapability
	}

	return a.execute(ctx, run, emit)
}

// Stream has an unbuffered event channel: an unread consumer blocks progress
// without accumulating events. Always drain Events or call Close. Wait returns
// after completion; calling Wait before draining requires cancellation.
type Stream[O any] struct {
	Events <-chan Event[O]
	cancel context.CancelFunc
	done   chan struct{}
	result Result[O]
	err    error
	once   sync.Once
}

func (s *Stream[O]) Close() { s.once.Do(s.cancel) }

func (s *Stream[O]) Wait() (Result[O], error) {
	<-s.done

	return s.result, s.err
}

func (a *Agent[D, O]) Stream(ctx context.Context, run Run[D]) *Stream[O] {
	if ctx == nil {
		events := make(chan Event[O])
		done := make(chan struct{})

		close(events)
		close(done)

		return &Stream[O]{
			Events: events, cancel: func() {}, done: done,
			result: Result[O]{Status: StatusFailed}, err: ErrConfig,
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	events := make(chan Event[O])

	s := &Stream[O]{Events: events, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer close(events)
		defer cancel()

		s.result, s.err = a.RunStream(ctx, run, func(event Event[O]) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case events <- event:
				return nil
			}
		})
	}()

	return s
}
