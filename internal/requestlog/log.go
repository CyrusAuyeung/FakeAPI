package requestlog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fakeapi/internal/config"
)

type Logger struct {
	mu     sync.Mutex
	out    io.Writer
	file   *os.File
	cfg    config.RequestLog
	errout io.Writer
}

func New(c config.RequestLog, out, errout io.Writer) (*Logger, error) {
	l := &Logger{cfg: c, out: out, errout: errout}
	if !c.Enabled {
		return l, nil
	}
	if c.Output != "stdout" {
		if e := os.MkdirAll(filepath.Dir(c.Output), 0700); e != nil {
			return nil, fmt.Errorf("E_LOG_OPEN")
		}
		f, e := os.OpenFile(c.Output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			return nil, fmt.Errorf("E_LOG_OPEN")
		}
		l.file = f
		l.out = f
	}
	return l, nil
}
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
	}
}
func (l *Logger) Write(start time.Time, model *string, duration time.Duration, rule *string) {
	if !l.cfg.Enabled {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	v := struct {
		Time     string  `json:"time"`
		Model    *string `json:"model"`
		Duration int64   `json:"duration_ms"`
		Rule     *string `json:"rule"`
	}{start.UTC().Format(time.RFC3339Nano), model, duration.Milliseconds(), rule}
	b, e := json.Marshal(v)
	if e == nil && l.file != nil {
		info, err := l.file.Stat()
		if err != nil {
			e = err
		} else if info.Size()+int64(len(b)) > l.cfg.MaxBytes {
			l.file.Close()
			for i := l.cfg.Backups - 1; i >= 1; i-- {
				_ = os.Rename(fmt.Sprintf("%s.%d", l.cfg.Output, i), fmt.Sprintf("%s.%d", l.cfg.Output, i+1))
			}
			_ = os.Rename(l.cfg.Output, l.cfg.Output+".1")
			l.file, e = os.OpenFile(l.cfg.Output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			l.out = l.file
		}
	}
	if e == nil {
		_, e = l.out.Write(append(b, '\n'))
	}
	if e != nil {
		fmt.Fprintln(l.errout, "E_REQUEST_LOG_WRITE")
	}
}
