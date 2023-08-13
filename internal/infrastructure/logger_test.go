package infrastructure

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = ginkgo.Describe("Logger", func() {
	ginkgo.Describe("initializeLog", func() {
		ginkgo.It("should initialize log with JSONFormatter", func() {
			initializeLog()
			gomega.Expect(log).ShouldNot(gomega.BeNil())
			_, ok := log.Formatter.(*logrus.JSONFormatter)
			gomega.Expect(ok).To(gomega.BeTrue(), "Expected JSONFormatter")
		})
	})

	ginkgo.Describe("GetLog", func() {
		ginkgo.It("should return initialized logger with JSONFormatter", func() {
			// Ensuring log is nil to test initialization
			log = nil

			logger := GetLog()
			gomega.Expect(logger).ShouldNot(gomega.BeNil())
			_, ok := logger.Formatter.(*logrus.JSONFormatter)
			gomega.Expect(ok).To(gomega.BeTrue(), "Expected JSONFormatter")

			// Ensuring that the logger returned is a singleton
			logger2 := GetLog()
			gomega.Expect(logger).To(gomega.Equal(logger2), "Expected the same logger instance")
		})
	})

	ginkgo.Describe("SetLog", func() {
		ginkgo.It("should set the logger", func() {
			SetLog(logrus.New())
			gomega.Expect(log).ShouldNot(gomega.BeNil())
		})
	})
})
