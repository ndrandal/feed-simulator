package config

import (
	"flag"
	"os"
	"strconv"
	"time"
)

// Config holds all simulator configuration.
type Config struct {
	// Server
	WSPort int
	Host   string

	// Database
	MongoURI string

	// Trade retention
	TradeRetentionDays int

	// Simulation
	Seed             int64
	TickInterval     time.Duration
	SnapshotInterval time.Duration
	SendBufferSize   int

	// S3 Glacier archiver (opt-in: only active when S3Bucket is set)
	S3Bucket             string
	S3Region             string
	S3Prefix             string
	ArchiveIntervalHours int
	ArchiveAfterHours    int

	// Stress
	StressCalmMinMs   int
	StressCalmMaxMs   int
	StressActiveMinMs int
	StressActiveMaxMs int
	StressBurstMinMs  int
	StressBurstMaxMs  int
}

func Load() *Config {
	c := &Config{}

	flag.IntVar(&c.WSPort, "port", envInt("FEED_PORT", 8100), "WebSocket server port")
	flag.StringVar(&c.Host, "host", envStr("FEED_HOST", "0.0.0.0"), "Listen host")

	flag.StringVar(&c.MongoURI, "mongo-uri", envStr("MONGO_URI", "mongodb://localhost:27017/feedsim"), "MongoDB connection URI")
	flag.IntVar(&c.TradeRetentionDays, "trade-retention", envInt("TRADE_RETENTION_DAYS", 7), "Trade log retention in days (0 = keep forever)")

	flag.StringVar(&c.S3Bucket, "s3-bucket", envStr("S3_BUCKET", ""), "S3 bucket for trade archival (empty = disabled)")
	flag.StringVar(&c.S3Region, "s3-region", envStr("S3_REGION", "us-east-1"), "AWS region for S3")
	flag.StringVar(&c.S3Prefix, "s3-prefix", envStr("S3_PREFIX", "feedsim"), "S3 key prefix for archived trades")
	flag.IntVar(&c.ArchiveIntervalHours, "archive-interval", envInt("ARCHIVE_INTERVAL_HOURS", 6), "Hours between archive runs")
	flag.IntVar(&c.ArchiveAfterHours, "archive-after", envInt("ARCHIVE_AFTER_HOURS", 24), "Archive trades older than this many hours")

	flag.Int64Var(&c.Seed, "seed", envInt64("FEED_SEED", 0), "PRNG seed (0 = random)")
	flag.IntVar(&c.SendBufferSize, "send-buffer", envInt("SEND_BUFFER", 4096), "Per-client send buffer size")

	flag.IntVar(&c.StressCalmMinMs, "stress-calm-min", 10, "Stress calm phase min tick ms")
	flag.IntVar(&c.StressCalmMaxMs, "stress-calm-max", 50, "Stress calm phase max tick ms")
	flag.IntVar(&c.StressActiveMinMs, "stress-active-min", 2, "Stress active phase min tick ms")
	flag.IntVar(&c.StressActiveMaxMs, "stress-active-max", 10, "Stress active phase max tick ms")
	flag.IntVar(&c.StressBurstMinMs, "stress-burst-min", 1, "Stress burst phase min tick ms")
	flag.IntVar(&c.StressBurstMaxMs, "stress-burst-max", 2, "Stress burst phase max tick ms")

	flag.Parse()

	c.TickInterval = 100 * time.Millisecond
	c.SnapshotInterval = 30 * time.Second

	return c
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
