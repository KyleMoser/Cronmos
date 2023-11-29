package logging

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func DoConfigureLogger(logLevel string) *zap.Logger {
	//Logger
	var logErr error
	cfg := zap.Config{
		OutputPaths: []string{"stdout", "log.txt"},
		EncoderConfig: zapcore.EncoderConfig{
			MessageKey:  "message",
			LevelKey:    "level",
			EncodeLevel: zapcore.LowercaseLevelEncoder,
		},
		Encoding: "json",
		Level:    zap.NewAtomicLevel(),
	}

	al, logErr := zap.ParseAtomicLevel(logLevel)
	if logErr != nil {
		os.Exit(1)
	}
	cfg.Level = al
	logger, logErr := cfg.Build()

	if logErr != nil {
		os.Exit(1)
	}

	return logger
}
