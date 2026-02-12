package helpers

import (
	"io"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var TestMode = true

func init() {
	// Disable all logging during tests
	zerolog.SetGlobalLevel(zerolog.Disabled)
	log.Logger = zerolog.New(io.Discard).Level(zerolog.Disabled)
}

func MainTestSetup(testingMain *testing.M) {
}

func MainTestTeardown(testingMain *testing.M) {
}

func SetupSuite(t *testing.T) {
	t.Log("Setting up test suite")
}

func TeardownSuite(t *testing.T) {
	t.Log("Tearing down test suite")
}
