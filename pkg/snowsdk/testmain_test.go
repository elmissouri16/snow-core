package snowsdk

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "snow-sdk-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("SNOW_HOME", home)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
