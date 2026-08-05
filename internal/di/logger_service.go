package di

import (
	"io"
	"log/slog"
	"os"

	"github.com/rs/zerolog"
	"github.com/samber/do/v2"
	slogzerolog "github.com/samber/slog-zerolog/v2"

	"github.com/omarluq/librecode/internal/config"
)

// LoggerService exposes structured slog and zerolog loggers.
type LoggerService struct {
	SlogLogger    *slog.Logger
	ZerologLogger zerolog.Logger
}

// NewLoggerService configures application logging from the resolved config.
func NewLoggerService(injector do.Injector) (*LoggerService, error) {
	configService := do.MustInvoke[*ConfigService](injector)
	cfg := configService.Get()
	zerologLogger := newZerologLogger(cfg, loggerWriter(configService))

	logger := slog.New(slogzerolog.Option{
		Level:  slogLevel(cfg.Logging.Level),
		Logger: &zerologLogger,
	}.NewZerologHandler()).With(slog.String("app", cfg.App.Name))

	slog.SetDefault(logger)

	return &LoggerService{
		SlogLogger:    logger,
		ZerologLogger: zerologLogger,
	}, nil
}

func loggerWriter(configService *ConfigService) io.Writer {
	if configService.Interactive() {
		return io.Discard
	}

	return os.Stdout
}

func newZerologLogger(cfg *config.Config, writer io.Writer) zerolog.Logger {
	level := parseZerologLevel(cfg.Logging.Level)

	if cfg.Logging.Format == "json" {
		return zerolog.New(writer).With().Timestamp().Logger().Level(level)
	}

	return zerolog.New(zerolog.ConsoleWriter{Out: writer}).With().Timestamp().Logger().Level(level)
}

func parseZerologLevel(level string) zerolog.Level {
	switch level {
	case "debug":
		return zerolog.DebugLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

func slogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
