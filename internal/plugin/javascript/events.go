package javascript

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/dop251/goja"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var promiseType = reflect.TypeFor[*goja.Promise]()

type subscription struct {
	handler  goja.Callable
	disabled bool
}

func (r *Runtime) subscribe(name string, value goja.Value) error {
	if !r.registering {
		return errors.New("on is only available during startup")
	}
	if len(r.subscriptions) >= MaxSubscriptions {
		return errors.New("plugin exceeds 128 subscriptions")
	}
	if !supportedEvent(plugin.EventType(name)) {
		if r.pkg.Manifest.APIVersion != 2 || !slices.Contains(protocol.KnownAgentEventTypes(), protocol.AgentEventType(name)) {
			return fmt.Errorf("unsupported observation event %q", name)
		}
	}
	if r.opts.ChildTools != nil {
		return nil
	}
	handler, ok := goja.AssertFunction(value)
	if !ok {
		return errors.New("event handler must be a function")
	}
	index := len(r.subscriptions)
	r.subscriptions = append(r.subscriptions, subscription{handler: handler})
	r.registrar.Subscribe(plugin.EventType(name), func(event plugin.Event) { r.enqueue(index, event) })
	return nil
}
func supportedEvent(t plugin.EventType) bool {
	switch t {
	case plugin.EventSessionUpdated, plugin.EventTextDelta, plugin.EventThinkingDelta, plugin.EventToolStart, plugin.EventToolProgress, plugin.EventToolEnd, plugin.EventToolRouting, plugin.EventPermissionRequest, plugin.EventUserInputRequest, plugin.EventUsage, plugin.EventTurnDone, plugin.EventError, plugin.EventAborted, plugin.EventModelChanged, plugin.EventModeChanged, plugin.EventPlanStarted, plugin.EventPlanDelta, plugin.EventPlanCompleted, plugin.EventPlanUpdate, plugin.EventCompactionStarted, plugin.EventCompactionDone, plugin.EventThreadGoalUpdated:
		return true
	}
	return false
}
func (r *Runtime) enqueue(sub int, event plugin.Event) {
	raw, err := jsonv2.Marshal(event)
	if err != nil {
		r.diagnostic("warning", "cannot encode plugin observation")
		return
	}
	r.mu.Lock()
	if r.closing || r.disabled != nil || r.observationsOff || r.frozen || r.retired {
		r.mu.Unlock()
		return
	}
	if len(r.queue) >= MaxQueueEvents || r.queueBytes+len(raw) > MaxQueueBytes {
		r.observationsOff = true
		r.queue = nil
		r.queueBytes = 0
		r.mu.Unlock()
		r.diagnostic("warning", "observation queue overflow; observers disabled for this session")
		return
	}
	r.queue = append(r.queue, observation{sub: sub, event: event, bytes: len(raw)})
	r.queueBytes += len(raw)
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
func (r *Runtime) pop() (observation, bool) {
	if r.pkg.Manifest.APIVersion == 2 && r.extension.observing {
		return observation{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queue) == 0 || r.frozen || r.retired {
		return observation{}, false
	}
	event := r.queue[0]
	r.queue[0] = observation{}
	r.queue = r.queue[1:]
	r.queueBytes -= event.bytes
	r.active++ // Reserve delivery before releasing the queue lock.
	return event, true
}
func (r *Runtime) deliver(event observation) {
	r.deliverContext(context.Background(), event)
}
func (r *Runtime) deliverContext(ctx context.Context, event observation) {
	if r.subscriptions[event.sub].disabled {
		r.endReloadActivity()
		return
	}
	if r.pkg.Manifest.APIVersion == 2 {
		r.extension.observing = true
		go func() {
			defer r.endReloadActivity()
			raw, err := jsonv2.Marshal(event.event)
			if err == nil {
				_, err = r.invokeAsync(ctx, string(event.event.Type), "observer", r.observerUses(), 5*time.Second, r.subscriptions[event.sub].handler, raw, nil)
			}
			_ = r.submit(r.extension.lifetime, time.Second, func() error {
				r.extension.observing = false
				if err != nil {
					r.subscriptions[event.sub].disabled = true
					r.diagnostic("warning", fmt.Sprintf("observer disabled: %v", err))
				}
				return nil
			})
		}()
		return
	}
	defer r.endReloadActivity()
	err := r.execute(work{ctx: ctx, budget: 100 * time.Millisecond, run: func() error {
		raw, err := jsonv2.Marshal(event.event)
		if err != nil {
			return err
		}
		value, err := r.decode(raw)
		if err != nil {
			return err
		}
		result, err := r.subscriptions[event.sub].handler(goja.Undefined(), value)
		if err != nil {
			return err
		}
		return r.requireSync(result)
	}})
	if err != nil {
		r.subscriptions[event.sub].disabled = true
		r.diagnostic("warning", fmt.Sprintf("observer disabled: %v", err))
	}
}
