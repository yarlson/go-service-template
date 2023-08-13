package infrastructure

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
)

var _ = Describe("IntRange", func() {
	Describe("Decode", func() {
		It("should decode a valid value", func() {
			ir := &IntRange{0, 0, 20}
			err := ir.Decode("10")
			Expect(err).NotTo(HaveOccurred())
			Expect(ir.Value).To(Equal(10))
		})

		It("should return an error for an out-of-range value", func() {
			ir := &IntRange{0, 0, 20}
			err := ir.Decode("25")
			Expect(err).To(HaveOccurred())
		})

		It("should return an error for an invalid integer", func() {
			ir := &IntRange{0, 0, 20}
			err := ir.Decode("abc")
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("StringEnum", func() {
	Describe("Decode", func() {
		It("should decode a valid value", func() {
			se := &StringEnum{"", []string{"debug", "info", "warn", "error"}}
			err := se.Decode("info")
			Expect(err).NotTo(HaveOccurred())
			Expect(se.Value).To(Equal("info"))
		})

		It("should return an error for an invalid value", func() {
			se := &StringEnum{"", []string{"debug", "info", "warn", "error"}}
			err := se.Decode("trace")
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("Config", func() {
	Describe("NewConfig", func() {
		It("should initialize with default values", func() {
			cfg := NewConfig()
			Expect(cfg).NotTo(BeNil())
			Expect(cfg.Redis.Db.Min).To(Equal(0))
			Expect(cfg.Redis.Db.Max).To(Equal(15))
			Expect(cfg.App.LogLevel.Enum).To(Equal([]string{"debug", "info", "warn", "error"}))
		})
	})

	Describe("LoadDefaultConfig", func() {
		BeforeEach(func() {
			_ = os.Setenv("DATABASE_URL", "test_db_url")
			_ = os.Setenv("REDIS_HOST", "localhost")
			_ = os.Setenv("REDIS_DB", "0")
			_ = os.Setenv("REDIS_PORT", "6379")
			_ = os.Setenv("JWT_PUBLIC_KEY", "test_key")
			_ = os.Setenv("LOG_LEVEL", "debug")
		})

		It("should load the default configuration", func() {
			cfg, err := LoadDefaultConfig()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Db.DatabaseUrl).To(Equal("test_db_url"))
			Expect(cfg.Redis.Host).To(Equal("localhost"))
			Expect(cfg.Redis.Db.Value).To(Equal(0))
			Expect(cfg.App.JwtPublicKey).To(Equal("test_key"))
			Expect(cfg.App.LogLevel.Value).To(Equal("debug"))
		})

		It("should return an error when required fields are missing", func() {
			_ = os.Unsetenv("REDIS_PORT")
			_, err := LoadDefaultConfig()
			Expect(err).To(HaveOccurred())
		})
	})
})
