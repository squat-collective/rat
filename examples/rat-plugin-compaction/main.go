// Command compaction is a RAT plugin that monitors Iceberg tables for
// small-file accumulation and rewrites them via PyIceberg's overwrite()
// workaround. Detection runs in-process (Go, walks Nessie + S3); the
// actual rewrite is delegated to compact.py which uses pyiceberg.
//
// Environment:
//
//	RATD_URL          ratd base URL              (default http://ratd:8080)
//	RATD_INTERNAL_URL ratd internal base URL     (default = RATD_URL)
//	GRPC_PORT     HTTP port to serve on          (default 50110)
//	PLUGIN_NAME   registered plugin name         (default compaction)
//	PLUGIN_ADDR   address ratd dials back        (default compaction:50110)
//
//	NESSIE_URL    Nessie base URL                (required)
//	S3_ENDPOINT   MinIO/S3 endpoint              (required)
//	S3_ACCESS_KEY                                 (required)
//	S3_SECRET_KEY                                 (required)
//	S3_BUCKET     bucket name                    (default rat)
//	S3_REGION                                    (default us-east-1)
//
//	COMPACT_INTERVAL_SECS     detection sweep cadence            (default 600)
//	COMPACT_TARGET_FILE_BYTES well-sized file target             (default 16 MiB)
//	COMPACT_MIN_FILE_COUNT    skip tables under this count       (default 50)
//	COMPACT_RATIO             undersize-score required to fire   (default 0.3)
//	COMPACT_AUTO              "true" enables auto-loop           (default true)
//	COMPACT_TIMEOUT_SECS      per-table compact timeout          (default 300)
package main

import (
	"context"
	_ "embed"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	sdk "github.com/rat-data/rat/sdk-go"
)

//go:embed bundle.js
var bundleJS []byte

var bundleHash = sdk.SRIHash(bundleJS)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	env := sdk.LoadPluginEnv("compaction", "50110", "compaction:50110")

	nessieURL := requireEnv("NESSIE_URL")
	s3Endpoint := requireEnv("S3_ENDPOINT")
	s3Access := requireEnv("S3_ACCESS_KEY")
	s3Secret := requireEnv("S3_SECRET_KEY")
	s3Bucket := envOr("S3_BUCKET", "rat")
	s3Region := envOr("S3_REGION", "us-east-1")

	interval := time.Duration(envInt("COMPACT_INTERVAL_SECS", 600)) * time.Second
	// targetFileBytes is the "well-sized" target — files at or above this
	// size are not considered small. Default 16 MiB is a pragmatic middle
	// ground for the bronze tables RAT typically holds; larger than this
	// and the rewrite_data_files (overwrite-based here) cost gets unfun.
	targetFileBytes := int64(envInt("COMPACT_TARGET_FILE_BYTES", 16<<20))
	minFileCount := envInt("COMPACT_MIN_FILE_COUNT", 50)
	ratio := envFloat("COMPACT_RATIO", 0.3)
	auto := envBool("COMPACT_AUTO", true)
	timeout := time.Duration(envInt("COMPACT_TIMEOUT_SECS", 300)) * time.Second

	s3 := newS3Client(s3Endpoint, s3Access, s3Secret, s3Bucket, s3Region)
	det := newDetector(nessieURL, s3, targetFileBytes, minFileCount, ratio)

	pyEnv := append(os.Environ(),
		"NESSIE_URL="+nessieURL,
		"S3_ENDPOINT="+s3Endpoint,
		"S3_ACCESS_KEY="+s3Access,
		"S3_SECRET_KEY="+s3Secret,
		"S3_REGION="+s3Region,
	)
	cmp := newCompactor(det, pyEnv, timeout, auto, interval)

	platformToken := sdk.RandomToken()
	a := newAPI(det, cmp)
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, a.mux())

	slog.Info("starting compaction plugin",
		"port", env.Port, "ratd_url", env.RatdURL,
		"interval_secs", int(interval.Seconds()), "target_file_bytes", targetFileBytes,
		"min_file_count", minFileCount, "ratio", ratio, "auto", auto,
	)

	ctx := context.Background()
	go func() {
		sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)
		cmp.Loop(ctx)
	}()

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("missing required env", "key", key)
		os.Exit(1)
	}
	return v
}

func envOr(key, def string) string {
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

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1" || v == "yes"
	}
	return def
}
