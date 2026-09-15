package rpc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"
)

// catalogTransport initializes only the existing Server's transport fields.
// NewWithOptions cannot be used: it enables app-owned interactive brokers.
func catalogTransport(in io.Reader, out io.Writer) (*Server, error) {
	if in == nil || out == nil {
		return nil, errors.New("rpc: catalog requires input and output")
	}
	input, ok := in.(io.ReadCloser)
	if !ok {
		input = io.NopCloser(in)
	}
	independentInput := false
	switch typed := in.(type) {
	case *bytes.Buffer, *bytes.Reader, *strings.Reader, *io.PipeReader:
		independentInput = true
	case *os.File:
		if info, err := typed.Stat(); err == nil && info.Mode().IsRegular() {
			independentInput = true
		}
	}
	if declared, ok := in.(InterruptibleInput); ok && declared.InterruptsReadOnClose() {
		independentInput = true
	}
	var deadline func(time.Time) error
	if setter, ok := in.(interface{ SetReadDeadline(time.Time) error }); ok {
		deadline = setter.SetReadDeadline
	}
	if !independentInput {
		if deadline == nil {
			return nil, errors.New("rpc: input must be finite or guarantee that Close/deadline interrupts reads")
		}
		if err := deadline(time.Now()); err != nil {
			return nil, fmt.Errorf("rpc: input read deadline unavailable: %w", err)
		}
		if err := deadline(time.Time{}); err != nil {
			return nil, fmt.Errorf("rpc: clear input read deadline: %w", err)
		}
	}
	independentOutput := false
	switch out.(type) {
	case *bytes.Buffer, *strings.Builder:
		independentOutput = true
	}
	if declared, ok := out.(BoundedOutput); ok && declared.RPCWriteBounded() {
		independentOutput = true
	}
	if typ := reflect.TypeOf(out); typ != nil && typ.Comparable() && out == io.Discard {
		independentOutput = true
	}
	_, deadlineOutput := out.(interface{ SetWriteDeadline(time.Time) error })
	if !independentOutput && !deadlineOutput {
		return nil, errors.New("rpc: output must be deadline-capable or guarantee bounded writes")
	}
	return &Server{in: input, inputDeadline: deadline, out: out, outputBounded: true, outputIndependentBound: independentOutput, writeFailed: make(chan struct{})}, nil
}
