package utils

import (
	"log/slog"

	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
)

func GetLogger(zapL *zap.Logger) *slog.Logger {
	return slog.New(zapslog.NewHandler(zapL.Core(), nil))
}

func GetZapLogger(dev bool) *zap.Logger {
	var zapL *zap.Logger
	if dev {
		zapL = zap.Must(zap.NewDevelopment())
	} else {
		zapL = zap.Must(zap.NewProduction())
	}
	return zapL
}
