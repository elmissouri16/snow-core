package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
)

const defaultLazyIdleTimeout = 10 * time.Minute

type runtimeState uint8

const (
	stateConfigured runtimeState = iota
	stateCached
	stateConnecting
	stateConnected
	stateDisconnecting
	stateFailed
	stateClosed
)

func (s runtimeState) String() string {
	switch s {
	case stateConfigured:
		return "configured"
	case stateCached:
		return "cached"
	case stateConnecting:
		return "connecting"
	case stateConnected:
		return "connected"
	case stateDisconnecting:
		return "disconnecting"
	case stateFailed:
		return "failed"
	case stateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

func (rt *serverRuntime) configuredLazy() bool {
	return rt.spec.Lifecycle == publicmcp.LifecycleLazy || rt.spec.Lifecycle == publicmcp.LifecycleLazyKeepAlive
}

func (rt *serverRuntime) lazy() bool {
	return rt.spec.Lifecycle == publicmcp.LifecycleLazy && rt.lazyEligible
}

func (rt *serverRuntime) strictNoBootstrap() bool {
	return rt.configuredLazy() && rt.spec.CacheBootstrap == publicmcp.CacheBootstrapExplicit
}

func (rt *serverRuntime) idleTimeout() time.Duration {
	if rt.spec.IdleTimeoutMS > 0 {
		return time.Duration(rt.spec.IdleTimeoutMS) * time.Millisecond
	}
	return defaultLazyIdleTimeout
}

// acquire returns one lease on a fully initialized live session. The connection
// attempt belongs to the runtime rather than to an individual waiting caller.
func (rt *serverRuntime) acquire(ctx context.Context) (*sdkmcp.ClientSession, func(), error) {
	for {
		rt.mu.Lock()
		switch rt.state {
		case stateClosed:
			rt.mu.Unlock()
			return nil, nil, errors.New("MCP runtime is closed")
		case stateConnected:
			session := rt.session
			if session == nil {
				rt.state = stateFailed
				rt.connectErr = errors.New("MCP connected state has no session")
				rt.mu.Unlock()
				continue
			}
			if rt.idleTimer != nil {
				rt.idleTimer.Stop()
				rt.idleTimer = nil
			}
			rt.activeCalls++
			rt.mu.Unlock()
			return session, sync.OnceFunc(rt.release), nil
		case stateConnecting, stateDisconnecting:
			attempt := rt.connectAttempt
			done := rt.transitionDone
			rt.mu.Unlock()
			if done == nil {
				continue
			}
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-done:
				if attempt != nil && attempt.err != nil {
					return nil, nil, attempt.err
				}
				continue
			}
		default:
			attempt := &runtimeConnectAttempt{done: make(chan struct{})}
			rt.state = stateConnecting
			rt.connectAttempt = attempt
			rt.transitionDone = attempt.done
			rt.connectErr = nil
			rt.mu.Unlock()
			go rt.finishConnect(attempt)
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-attempt.done:
				if attempt.err != nil {
					return nil, nil, attempt.err
				}
			}
		}
	}
}

func (rt *serverRuntime) finishConnect(attempt *runtimeConnectAttempt) {
	err := rt.connectLive(rt.runtimeCtx)
	rt.mu.Lock()
	attempt.err = err
	if rt.state == stateClosed {
		// Final close owns any session that managed to publish before cancellation.
	} else if err != nil {
		rt.state = stateFailed
		rt.connectErr = err
	} else {
		rt.state = stateConnected
		rt.connectErr = nil
		rt.armIdleLocked()
	}
	if rt.connectAttempt == attempt {
		rt.connectAttempt = nil
	}
	if rt.transitionDone == attempt.done {
		rt.transitionDone = nil
		close(attempt.done)
	}
	rt.mu.Unlock()
	if err != nil {
		rt.manager.setRuntimeMessage(rt.spec.ID, err.Error())
	} else {
		rt.manager.updateRuntimeStatus(rt, "")
	}
}

func (rt *serverRuntime) release() {
	rt.mu.Lock()
	if rt.activeCalls > 0 {
		rt.activeCalls--
	}
	rt.lastUsed = rt.manager.now().UTC()
	rt.armIdleLocked()
	rt.mu.Unlock()
	rt.manager.updateRuntimeStatus(rt, "")
}

// armIdleLocked schedules an idle close only for genuinely lazy, connected,
// unleased tool-only runtimes. rt.mu must be held.
func (rt *serverRuntime) armIdleLocked() {
	if !rt.lazy() || rt.state != stateConnected || rt.activeCalls != 0 || rt.refreshing {
		return
	}
	generation := rt.generation
	if rt.idleTimer != nil {
		rt.idleTimer.Stop()
	}
	rt.idleTimer = time.AfterFunc(rt.idleTimeout(), func() { rt.idleDisconnect(generation) })
}

func (rt *serverRuntime) idleDisconnect(generation uint64) {
	rt.mu.Lock()
	if rt.state != stateConnected || rt.generation != generation || rt.activeCalls != 0 || rt.refreshing {
		rt.mu.Unlock()
		return
	}
	done := make(chan struct{})
	rt.state = stateDisconnecting
	rt.transitionDone = done
	rt.idleTimer = nil
	rt.mu.Unlock()

	err := rt.disconnectLive(false)
	rt.mu.Lock()
	if rt.state != stateClosed {
		if rt.cached.valid() {
			rt.state = stateCached
		} else {
			rt.state = stateConfigured
		}
	}
	if rt.transitionDone == done {
		rt.transitionDone = nil
		close(done)
	}
	rt.mu.Unlock()
	message := "idle-disconnected"
	if err != nil {
		message = rt.safeRuntimeError("idle disconnect", err).Error()
	}
	rt.manager.updateRuntimeStatus(rt, message)
}

func (rt *serverRuntime) liveBridgeAvailable(kind string) bool {
	required := ""
	switch kind {
	case "list_resources", "read_resource":
		required = "resources"
	case "resource_subscription":
		required = "resources.subscribe"
	case "list_prompts", "get_prompt":
		required = "prompts"
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return required != "" && rt.liveCapabilities[required] && rt.session != nil && rt.state == stateConnected
}

func (rt *serverRuntime) liveToolMatches(name string, schema json.RawMessage) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	liveSchema, ok := rt.liveTools[name]
	return ok && liveSchema == string(schema) && rt.session != nil && rt.state == stateConnected
}
