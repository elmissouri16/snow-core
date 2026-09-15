package web

import (
	"os"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func runtimeCancelFixtureGate(suffix string) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(os.Getenv("SNOW_WEB_RUNTIME_TEST_LOG") + suffix); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	os.Exit(19)
}

func runtimeCancelPromptFixture(request protocol.RPCRequest, response protocol.RPCResponse, emit func(any), complete func(protocol.RPCPromptStatus)) {
	runtimeCancelFixtureGate(".ack")
	switch request.Message {
	case "cancel-complete":
		complete(protocol.RPCPromptCompletedStatus)
	case "cancel-exit":
		os.Exit(17)
	case "cancel-reject":
		response.Success = false
	}
	emit(response)
}

func runtimeCancelAbortFixture(mode string, response protocol.RPCResponse, emit func(any), complete func(protocol.RPCPromptStatus)) {
	switch mode {
	case "cancel-complete-before-abort-ack":
		complete(protocol.RPCPromptCanceledStatus)
		runtimeCancelFixtureGate(".abort-ack")
		emit(response)
	case "cancel-abort-exit":
		os.Exit(17)
	case "cancel-abort-reject":
		response.Success = false
		emit(response)
	default:
		emit(response)
		runtimeCancelFixtureGate(".complete")
		complete(protocol.RPCPromptCanceledStatus)
	}
}
