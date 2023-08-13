package api

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"go-service-template/internal/infrastructure"
	"net/http"
	"net/http/httptest"
)

var _ = Describe("LoggingMiddleware", func() {
	It("should log the request and call the next handler", func() {
		// Creating a test logger and replacing the infrastructure log with it
		testLogger, hook := test.NewNullLogger()
		infrastructure.SetLog(testLogger) // You'll need to create this method to set the logger in the infrastructure package

		// Creating a sample next handler
		nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("next handler"))
		})

		// Creating a request to pass to the handler
		req, err := http.NewRequest("GET", "/test", nil)
		req.RequestURI = "/test" // Explicitly setting the RequestURI field
		Expect(err).NotTo(HaveOccurred())

		// Creating a ResponseRecorder to record the response
		rr := httptest.NewRecorder()

		// Creating the middleware handler
		handler := LoggingMiddleware(nextHandler)

		// Serve the request
		handler.ServeHTTP(rr, req)

		// Checking if next handler was called
		Expect(rr.Body.String()).To(Equal("next handler"))

		// Checking if the log entry was made
		Expect(len(hook.Entries)).To(Equal(1))
		entry := hook.LastEntry()
		Expect(entry.Level).To(Equal(logrus.InfoLevel))
		Expect(entry.Message).To(Equal("Handled request"))
		Expect(entry.Data["method"]).To(Equal("GET"))
		Expect(entry.Data["uri"]).To(Equal("/test")) // Checking for the expected URI
		Expect(entry.Data["remote"]).To(Equal(req.RemoteAddr))

		// Since we don't control the exact time duration, we'll just check if it exists
		_, durationExists := entry.Data["duration"]
		Expect(durationExists).To(BeTrue(), "Duration should be logged")
	})
})
