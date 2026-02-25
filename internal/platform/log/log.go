package applog

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"

	slogzap "github.com/samber/slog-zap/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Config 日志服务配置。
type Config struct {
	Level     string
	Format    string // text | json
	AddSource bool
	Output    io.Writer
}

var (
	zapLogger *zap.Logger
	mu        sync.RWMutex
)

// Init 初始化全局日志服务（zap + slog bridge）。
func Init(cfg Config) {
	logger := buildZapLogger(cfg)

	mu.Lock()
	zapLogger = logger
	mu.Unlock()

	zap.ReplaceGlobals(logger)

	slogHandler := slogzap.Option{
		Level:     parseSlogLevel(cfg.Level),
		Logger:    logger,
		AddSource: cfg.AddSource,
	}.NewZapHandler()
	slog.SetDefault(slog.New(slogHandler))

	log.SetOutput(cfg.OutputOrStdout())
	log.SetFlags(0)
}

// Zap 返回全局 zap logger。
func Zap() *zap.Logger {
	mu.RLock()
	defer mu.RUnlock()
	if zapLogger != nil {
		return zapLogger
	}
	return zap.L()
}

// With 返回带默认字段的 slog logger。
func With(args ...any) *slog.Logger {
	return slog.Default().With(args...)
}

func Debug(msg string, args ...any) { slog.Debug(msg, args...) }
func Info(msg string, args ...any)  { slog.Info(msg, args...) }
func Warn(msg string, args ...any)  { slog.Warn(msg, args...) }
func Error(msg string, args ...any) { slog.Error(msg, args...) }

func Debugf(format string, args ...any) { slog.Debug(fmt.Sprintf(format, args...)) }
func Infof(format string, args ...any)  { slog.Info(fmt.Sprintf(format, args...)) }
func Warnf(format string, args ...any)  { slog.Warn(fmt.Sprintf(format, args...)) }
func Errorf(format string, args ...any) { slog.Error(fmt.Sprintf(format, args...)) }

func Fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

func Fatalf(format string, args ...any) {
	slog.Error(fmt.Sprintf(format, args...))
	os.Exit(1)
}

func buildZapLogger(cfg Config) *zap.Logger {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.TimeKey = "time"

	var encoder zapcore.Encoder
	if strings.EqualFold(cfg.Format, "json") {
		encoder = zapcore.NewJSONEncoder(encoderCfg)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(cfg.OutputOrStdout()),
		parseZapLevel(cfg.Level),
	)

	options := []zap.Option{zap.AddStacktrace(zapcore.ErrorLevel)}
	if cfg.AddSource {
		options = append(options, zap.AddCaller())
	}

	return zap.New(core, options...)
}

func (c Config) OutputOrStdout() io.Writer {
	if c.Output == nil {
		return os.Stdout
	}
	return c.Output
}

func parseSlogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
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

func parseZapLevel(level string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return zapcore.DebugLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
