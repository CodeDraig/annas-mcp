package logger

import (
	"log"

	"go.uber.org/zap"
)

var logger *zap.Logger

func init() {
	config := zap.NewDevelopmentConfig()
	config.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	var err error
	logger, err = config.Build()

	if err != nil {
		log.Fatalf("Failed to initialize zap logger: %v", err)
	}
}

func GetLogger() *zap.Logger {
	return logger
}
