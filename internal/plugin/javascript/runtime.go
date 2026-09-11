package javascript

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"
	"github.com/elmissouri16/snow-core/pkg/plugin"
)

type Options struct {
	MaxOutputBytes   int
	MaxProgressBytes int
	// Diagnostic must return promptly and must not re-enter the runtime.
	Diagnostic func(string, string)
	ChildTools []string // nil denotes the root profile
}

type work struct {
	ctx     context.Context
	budget  time.Duration
	run     func() error
	done    chan error
	closing bool
}

type observation struct {
	sub   int
	event plugin.Event
	bytes int
}

// Runtime owns a single worker. VM values never leave that worker.
type Runtime struct {
	pkg             *Package
	opts            Options
	vm              *goja.Runtime
	requests        chan work
	wake            chan struct{}
	stop            chan struct{}
	done            chan struct{}
	mu              sync.Mutex
	started         bool
	closing         bool
	disabled        error
	cancel          context.CancelFunc
	queue           []observation
	queueBytes      int
	observationsOff bool
	logs            int
	diagnosticFn    func(string, string)
	active          int
	frozen          bool
	retired         bool
	stopOnce        sync.Once
	// Everything below is worker-owned.
	registrar        plugin.Registrar
	registering      bool
	childDefinitions []toolDefinition
	tools            int
	subscriptions    []subscription
	onClose          goja.Callable
	currentContext   context.Context
	stringify        goja.Callable
	parse            goja.Callable
	lastThrown       *goja.Object
	lastError        string
	extension        *extensionState
}

func New(p *Package, opts Options) *Runtime {
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = 256 << 10
	}
	if opts.MaxProgressBytes <= 0 {
		opts.MaxProgressBytes = 16 << 10
	}
	r := &Runtime{pkg: clonePackage(p), opts: opts, requests: make(chan work), wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	r.diagnosticFn = opts.Diagnostic
	r.extension = newExtensionState()
	return r
}

func (r *Runtime) Manifest() plugin.Manifest {
	return plugin.Manifest{ID: r.pkg.Manifest.ID, Name: r.pkg.Manifest.Name, Version: r.pkg.Manifest.Version, ProtocolVersion: plugin.ProtocolVersion}
}

func (r *Runtime) Register(ctx context.Context, registrar plugin.Registrar) error {
	r.mu.Lock()
	if r.started || r.closing {
		r.mu.Unlock()
		return errors.New("JavaScript runtime already initialized or closed")
	}
	r.started = true
	r.mu.Unlock()
	go r.worker()
	return r.submit(ctx, time.Second, func() error {
		r.registrar = registrar
		r.registering = true
		defer func() { r.registering = false; r.registrar = nil }()
		if err := r.installAPI(); err != nil {
			return err
		}
		_, err := r.vm.RunScript(r.pkg.Manifest.Entry, string(r.pkg.Script))
		return err
	})
}

func (r *Runtime) worker() {
	defer close(r.done)
	for {
		// Tools have priority over pending observation work.
		select {
		case <-r.stop:
			return
		case job := <-r.requests:
			job.done <- r.runWork(job)
			r.flushSettled()
			continue
		default:
		}
		if event, ok := r.pop(); ok {
			r.deliver(event)
			continue
		}
		select {
		case <-r.stop:
			return
		case job := <-r.requests:
			job.done <- r.runWork(job)
			r.flushSettled()
		case <-r.wake:
		}
	}
}

func (r *Runtime) submit(ctx context.Context, budget time.Duration, run func() error) error {
	if err := r.beginReloadActivity(); err != nil {
		return err
	}
	defer r.endReloadActivity()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	job := work{ctx: ctx, budget: budget, run: run, done: make(chan error, 1)}
	r.mu.Lock()
	closing, disabled := r.closing, r.disabled
	r.mu.Unlock()
	if closing {
		return errors.New("JavaScript plugin is closed")
	}
	if disabled != nil {
		return fmt.Errorf("JavaScript plugin disabled: %w", disabled)
	}
	select {
	case r.requests <- job:
	case <-ctx.Done():
		return ctx.Err()
	case <-r.done:
		return errors.New("JavaScript plugin is closed")
	}
	// After admission, the worker owns all VM state until cancellation has fully
	// unwound. Do not return while a late host operation could still mutate it.
	select {
	case err := <-job.done:
		return err
	case <-r.done:
		return errors.New("JavaScript plugin is closed")
	}
}

func (r *Runtime) execute(job work) (err error) {
	r.mu.Lock()
	if r.disabled != nil {
		err = fmt.Errorf("JavaScript plugin disabled: %w", r.disabled)
		r.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithTimeout(job.ctx, job.budget)
	r.cancel = cancel
	r.mu.Unlock()
	defer func() { cancel(); r.mu.Lock(); r.cancel = nil; r.mu.Unlock() }()
	r.currentContext = ctx
	defer func() { r.currentContext = nil }()
	// VM creation occurs in the first job; the watcher reads the VM only after
	// setup through the worker's explicit initialization below.
	if r.vm == nil {
		r.vm = goja.New()
		r.vm.SetMaxCallStackSize(256)
		r.vm.SetParserOptions(parser.WithDisableSourceMaps)
		if r.pkg.Manifest.APIVersion == 2 {
			r.vm.SetAsyncContextTracker(&asyncTracker{state: r.extension})
		}
	}
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { r.vm.Interrupt(ctx.Err()); close(finished) })
	defer func() {
		if !stop() {
			<-finished
		}
		r.vm.ClearInterrupt()
		if recovered := recover(); recovered != nil {
			err = errors.New("JavaScript host callback panicked")
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		_, interrupted := errors.AsType[*goja.InterruptedError](err)
		_, overflow := errors.AsType[*goja.StackOverflowError](err)
		if overflow {
			err = errors.New("JavaScript call stack limit exceeded")
		}
		if interrupted && ctx.Err() == nil {
			err = errors.New("JavaScript execution interrupted")
		}
		if ctx.Err() != nil || interrupted || overflow {
			r.disable(err)
		}
		err = r.plainError(err)
	}()
	if err = ctx.Err(); err != nil {
		return err
	}
	err = job.run()
	return r.copyError(err)
}

func (r *Runtime) disable(err error) {
	r.extension.cancel()
	r.mu.Lock()
	first := r.disabled == nil
	r.disabled = err
	r.queue = nil
	r.queueBytes = 0
	r.observationsOff = true
	r.mu.Unlock()
	if first {
		r.diagnostic("disabled", fmt.Sprintf("plugin disabled for this session: %v", err))
	}
}

func (r *Runtime) Close(ctx context.Context) error {
	r.extension.cancel()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	r.mu.Lock()
	if !r.started {
		r.closing = true
		r.mu.Unlock()
		return nil
	}
	if r.closing {
		r.mu.Unlock()
		select {
		case <-r.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.closing = true
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	// A dedicated close job runs on the same worker, after any active call.
	job := work{ctx: ctx, budget: time.Second, done: make(chan error, 1), closing: true, run: func() error {
		if r.onClose != nil {
			v, err := r.onClose(goja.Undefined())
			if err != nil {
				return err
			}
			return r.requireSync(v)
		}
		return nil
	}}
	var err error
	select {
	case r.requests <- job:
		select {
		case err = <-job.done:
		case <-ctx.Done():
			err = ctx.Err()
		}
	case <-r.done:
		return nil
	case <-ctx.Done():
		err = ctx.Err()
	}
	r.mu.Lock()
	discarded := len(r.queue)
	r.queue = nil
	r.queueBytes = 0
	r.observationsOff = true
	r.mu.Unlock()
	if discarded > 0 {
		r.diagnostic("warning", fmt.Sprintf("discarded %d observations on close", discarded))
	}
	r.stopOnce.Do(func() { close(r.stop) })
	select {
	case <-r.done:
	case <-ctx.Done():
		if err == nil {
			err = ctx.Err()
		}
	}
	return err
}

func (r *Runtime) diagnostic(level, message string) {
	r.mu.Lock()
	fn := r.diagnosticFn
	r.mu.Unlock()
	if fn != nil {
		fn(level, boundText(message, MaxLogBytes))
	}
}
func boundText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

func (r *Runtime) runWork(job work) error {
	r.mu.Lock()
	closing := r.closing
	r.mu.Unlock()
	if closing && !job.closing {
		return errors.New("JavaScript plugin is closing")
	}
	if job.closing {
		for {
			if job.ctx.Err() != nil {
				return job.ctx.Err()
			}
			event, ok := r.pop()
			if !ok {
				break
			}
			r.deliverContext(job.ctx, event)
		}
		r.mu.Lock()
		disabled := r.disabled
		r.mu.Unlock()
		if disabled != nil {
			return nil
		}
	}
	return r.execute(job)
}

// No exception retaining VM-owned objects may cross the worker boundary.
func (r *Runtime) plainError(err error) error {
	if err == nil {
		return nil
	}
	if ex, ok := errors.AsType[*goja.Exception](err); ok {
		message := "JavaScript exception"
		if obj, ok := ex.Value().(*goja.Object); ok {
			if obj == r.lastThrown {
				message = r.lastError
			}
		} else {
			message = boundText(ex.Value().String(), MaxLogBytes)
		}
		if stack := ex.Stack(); len(stack) > 0 {
			position := stack[0].Position()
			message = fmt.Sprintf("%s (%s:%d:%d)", message, stack[0].SrcName(), position.Line, position.Column)
		}
		return errors.New(message)
	}
	return err
}

// Inspect a thrown message only while the watchdog is active. Accessors can
// execute JavaScript, so even diagnostic extraction belongs on the worker.
func (r *Runtime) copyError(err error) error {
	ex, ok := errors.AsType[*goja.Exception](err)
	if !ok {
		return err
	}
	if obj, ok := ex.Value().(*goja.Object); ok && obj != r.lastThrown {
		var message string
		caught := r.vm.Try(func() {
			v := obj.Get("message")
			if v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
				if _, object := v.(*goja.Object); !object {
					message = boundText(v.String(), MaxLogBytes)
				}
			}
		})
		if caught == nil && message != "" {
			return fmt.Errorf("JavaScript exception: %s", message)
		}
	}
	return r.plainError(err)
}
