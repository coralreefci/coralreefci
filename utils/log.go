package utils

import (
	"log"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/johntdyer/slackrus"
	"go.uber.org/zap"
)

var (
	// AppLog is a public application-focused logger
	AppLog *zap.Logger
	// ModelLog is a public model-focused logger
	ModelLog *zap.Logger
	// SlackLog is a public Slack channel log poster
	SlackLog *logrus.Logger
)

var initOnceLog sync.Once

func init() {
	initOnceLog.Do(func() {
		AppLog = intializeLog(Config.AppLogPath)
		ModelLog = intializeLog(Config.ModelLogPath)
		SlackLog = initializeSlackLog()
	})
}

func intializeLog(logPath string) *zap.Logger {
	logConfig := zap.NewProductionConfig()
	logConfig.OutputPaths = []string{logPath}
	logConfig.Sampling = nil
	logger, err := logConfig.Build()
	if err != nil {
		log.Fatal(err)
	}
	return logger
}

func initializeSlackLog() *logrus.Logger {
	logInstance := logrus.New()
	logInstance.Formatter = new(logrus.TextFormatter)
	logInstance.Hooks.Add(&slackrus.SlackrusHook{
		HookURL:        "https://hooks.slack.com/services/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",
		AcceptedLevels: slackrus.LevelThreshold(logrus.InfoLevel),
		Channel:        "#status",
		Username:       "status-update",
	})
	return logInstance
}
