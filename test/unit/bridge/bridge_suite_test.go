package bridge_test

import (
	"testing"

	_ "nodefy/agent/test/helpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBridge(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bridge Suite")
}
