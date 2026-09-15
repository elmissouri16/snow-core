package rpc

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/elmissouri16/snow-core/internal/hostcontrol"
	"github.com/elmissouri16/snow-core/internal/hostops"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const controlOperationTimeout = 3 * time.Second
const controlReadTimeout = 5 * time.Second
const controlWriteTimeout = 5 * time.Second

var errControlUnavailable = errors.New("control unavailable")

// ControlService is the narrow runtime-free host facade. In particular it has
// no login, refresh, model discovery, provider or extension operations.
// Optional inactive-session deletion is separately composed and capability-gated.
type ControlService interface {
	GetDefaults(context.Context, protocol.HostDefaultsRequest) (protocol.HostDefaultsResponse, error)
	UpdateDefaults(context.Context, protocol.HostDefaultsUpdateRequest) (protocol.HostDefaultsResponse, error)
	ProviderStatus(context.Context) (protocol.HostProviderStatusResponse, error)
}

// ControlMain composes operator-owned paths without loading the application.
func ControlMain(ctx context.Context, cwd, version string) error {
	service, err := hostcontrol.New(hostcontrol.Options{})
	if err != nil {
		return errControlUnavailable
	}
	out := &processOutputWriter{out: os.Stdout, timeout: controlWriteTimeout}
	options := ControlOptions{SessionDelete: defaultSessionDeleteControl(), HostOperations: NewControlHostOperations(hostops.Options{GitExecutable: "/usr/bin/git"})}
	if keys, ok := any(service).(APIKeyControlService); ok {
		options.APIKeys = keys
	}
	return ServeControl(ctx, newProcessInputReader(os.Stdin), out, cwd, version, service, options)
}

// ServeControl executes a separate serial cold command loop. Input ownership and
// bounded output requirements match catalogTransport; no live Server is built.
// cwd is launch-selected, not a request-selected filesystem authority.
func ServeControl(ctx context.Context, in io.Reader, out io.Writer, cwd, version string, service ControlService, options ...ControlOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pin, err := newControlProjectPin(cwd)
	if err != nil || service == nil {
		return errControlUnavailable
	}
	srv, err := catalogTransport(in, out)
	if err != nil {
		return errControlUnavailable
	}
	// The ordinary runtime uses a shorter write deadline. Keep this cold
	// transport on its explicit five-second contract without changing it.
	srv.out = controlOutputWriter{out: srv.out, independent: srv.outputIndependentBound}
	var option ControlOptions
	if len(options) > 1 {
		return errControlUnavailable
	}
	if len(options) == 1 {
		option = options[0]
	}
	return serveControlLoop(ctx, srv, pin, service, version, option)
}

func writeControl(srv *Server, value any) error {
	data, err := json.Marshal(value)
	if err != nil || len(data)+1 > protocol.RPCControlMaxOutputBytes {
		failure := controlFailure("", "invalid", "unavailable")
		if response, ok := value.(Response); ok {
			failure.ID, failure.Command = response.ID, response.Command
		}
		value = failure
	}
	if err := srv.write(value); err != nil {
		return errControlUnavailable
	}
	return nil
}

type controlProjectPin struct {
	cwd  string
	info os.FileInfo
}

func newControlProjectPin(cwd string) (controlProjectPin, error) {
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return controlProjectPin{}, errControlUnavailable
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return controlProjectPin{}, errControlUnavailable
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return controlProjectPin{}, errControlUnavailable
	}
	return controlProjectPin{cwd: canonical, info: info}, nil
}

func (p controlProjectPin) matches(scope, cwd string) bool {
	if scope == "global" {
		return cwd == ""
	}
	if scope != "project" || cwd != p.cwd {
		return false
	}
	canonical, err := filepath.EvalSymlinks(p.cwd)
	if err != nil || canonical != p.cwd {
		return false
	}
	info, err := os.Stat(p.cwd)
	return err == nil && info.IsDir() && os.SameFile(p.info, info)
}

// controlOutputWriter hides the live transport's shorter deadline setter and
// applies the cold worker deadline to the already-validated output contract.
type controlOutputWriter struct {
	out         io.Writer
	independent bool
}

func (w controlOutputWriter) Write(data []byte) (int, error) {
	if setter, ok := w.out.(interface{ SetWriteDeadline(time.Time) error }); ok {
		if err := setter.SetWriteDeadline(time.Now().Add(controlWriteTimeout)); err != nil {
			if !w.independent {
				return 0, errControlUnavailable
			}
		} else {
			defer setter.SetWriteDeadline(time.Time{})
		}
	}
	return w.out.Write(data)
}
