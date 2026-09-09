package main

import (
	"context"
	"path/filepath"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

// newSlowSQLLogger 将运行期慢查询独立写入数据目录，开发模式也使用相同的轮转规则。
func newSlowSQLLogger(cfg *config.Config) (*zap.Logger, error) {
	if cfg.Logging.SlowSQLThresholdMS <= 0 {
		return zap.NewNop(), nil
	}
	writer, err := newRotatingFileWriter(filepath.Join(cfg.App.DataDir, "logs", "slow-sql.log"), cfg.Logging)
	if err != nil {
		return nil, err
	}
	encoder := zap.NewProductionEncoderConfig()
	encoder.EncodeTime = zapcore.ISO8601TimeEncoder
	return zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(encoder), writer, zap.WarnLevel)), nil
}

// slowSQLLogger 保留原有错误日志路径，成功的慢查询只写入专用日志。
type slowSQLLogger struct {
	logger.Interface
	log       *zap.Logger
	threshold time.Duration
	silent    bool
}

func (l slowSQLLogger) LogMode(level logger.LogLevel) logger.Interface {
	l.Interface = l.Interface.LogMode(level)
	l.silent = level == logger.Silent
	return l
}

func (l slowSQLLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.silent {
		return
	}
	if err != nil {
		l.Interface.Trace(ctx, begin, fc, err)
		return
	}
	elapsed := time.Since(begin)
	if l.threshold <= 0 || elapsed <= l.threshold {
		return
	}
	sql, rows := fc()
	l.log.Warn("slow SQL", zap.Float64("duration_ms", float64(elapsed)/float64(time.Millisecond)),
		zap.Int64("rows", rows), zap.String("source", utils.FileWithLineNum()), zap.String("sql", sql))
}

// ParamsFilter 不展开绑定参数，避免凭据、路径和个人信息进入 SQL 日志。
func (l slowSQLLogger) ParamsFilter(_ context.Context, sql string, _ ...interface{}) (string, []interface{}) {
	return sql, nil
}
