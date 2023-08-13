package api

import (
	"go-service-template/internal/infrastructure"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestInfrastructure(t *testing.T) {
	RegisterFailHandler(Fail)
	infrastructure.GetLog()
	RunSpecs(t, "API Suite")
}
