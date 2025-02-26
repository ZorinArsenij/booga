package booga

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"
)

// entry represents single mongo log entry.
//
// See https://docs.mongodb.com/manual/reference/log-messages/
type entry struct {
	Severity   string                 `json:"s"`
	System     string                 `json:"c"`
	ID         int                    `json:"id"`
	Context    string                 `json:"ctx"`
	Message    string                 `json:"msg"`
	Attributes map[string]interface{} `json:"attr"`

	T struct {
		Date time.Time `json:"$date"`
	} `json:"t"`
}

// Log writes entry to zap logger as structured log entry.
func (e *entry) Log(log *zap.Logger) {
	var severity zapcore.Level
	switch e.Severity {
	case "W":
		severity = zapcore.WarnLevel
	case "E", "F":
		// We can't use Fatal level because this will call os.Exit.
		severity = zapcore.ErrorLevel
	}
	if ce := log.Check(severity, e.Message); ce != nil {
		// We ignore time field here.
		fields := []zapcore.Field{
			zap.String("c", e.System),
			zap.Int("id", e.ID),
			zap.String("ctx", e.Context),
		}
		if len(e.Attributes) > 0 {
			fields = append(fields, zap.Any("attr", e.Attributes))
		}
		ce.Write(fields...)
	}
}

// logProxy returns io.Writer that can be used as mongo log output.
//
// The io.Writer will parse json logs and write them to provided logger.
// Call context.CancelFunc on mongo exit.
func logProxy(log *zap.Logger, g *errgroup.Group) (io.Writer, context.CancelFunc) {
	r, w := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())

	g.Go(func() error {
		<-ctx.Done()
		return r.Close()
	})
	g.Go(func() error {
		s := bufio.NewScanner(r)
		log.Info("Log streaming started")
		defer log.Info("Log streaming ended")
		for s.Scan() {
			var e entry
			if err := json.Unmarshal(s.Bytes(), &e); err != nil {
				log.Warn("Failed to unmarshal log entry", zap.Error(err))
				continue
			}
			e.Log(log)
		}
		return s.Err()
	})

	return w, cancel
}
