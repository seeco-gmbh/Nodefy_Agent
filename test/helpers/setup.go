package helpers

import (
	"io"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func init() {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	log.Logger = zerolog.New(io.Discard).Level(zerolog.Disabled)
}

func SetupSuite(t *testing.T) {
	t.Log("Setting up test suite")
}

func TeardownSuite(t *testing.T) {
	t.Log("Tearing down test suite")
}
