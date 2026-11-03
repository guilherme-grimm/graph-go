package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"go.uber.org/zap"
)

// watchEvents subscribes to Docker container start/stop/die events and
// invokes onChange for each one. It reconnects with exponential backoff
// and returns only when ctx is cancelled.
func watchEvents(ctx context.Context, cli *client.Client, onChange func(), logger *zap.SugaredLogger) error {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return nil
		}

		eventFilter := make(client.Filters).
			Add("type", string(events.ContainerEventType)).
			Add("event", "start", "stop", "die")
		stream := cli.Events(ctx, client.EventsListOptions{
			Filters: eventFilter,
		})

		start := time.Now()
		err := consumeEvents(ctx, stream.Messages, stream.Err, onChange, logger)
		if time.Since(start) > 10*time.Second {
			backoff = time.Second // only reset if stream was stable
		}
		if ctx.Err() != nil {
			return nil
		}

		logger.Warnw("docker event stream error", "err", err, "reconnect_in", backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// consumeEvents calls onChange for every Docker event until the context is
// cancelled (returning ctx.Err()) or the stream ends. A closed message
// channel, an error on errCh, or a nil error (the stream finished) all
// return an error so the caller can reconnect.
func consumeEvents(ctx context.Context, msgCh <-chan events.Message, errCh <-chan error, onChange func(), logger *zap.SugaredLogger) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-msgCh:
			if !ok {
				return fmt.Errorf("event stream closed")
			}
			logger.Debugw("docker event", "action", msg.Action, "type", msg.Type, "container", truncateID(msg.Actor.ID))
			onChange()
		case err := <-errCh:
			if err != nil {
				return err
			}
			return fmt.Errorf("event stream closed")
		}
	}
}
