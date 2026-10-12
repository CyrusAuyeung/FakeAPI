package requestlog

import (
	"bytes"
	"encoding/json"
	"fakeapi/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFourFieldsAndRotation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "requests.jsonl")
	out, errout := new(bytes.Buffer), new(bytes.Buffer)
	log, e := New(config.RequestLog{Enabled: true, Output: file, MaxBytes: 180, Backups: 2}, out, errout)
	if e != nil {
		t.Fatal(e)
	}
	model, rule := "fakeapi-test", "preset-ok"
	for i := 0; i < 8; i++ {
		log.Write(time.Unix(0, 0), &model, time.Millisecond, &rule)
	}
	log.Close()
	for _, suffix := range []string{"", ".1", ".2"} {
		b, e := os.ReadFile(file + suffix)
		if e != nil {
			t.Fatal(e)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var m map[string]any
			if json.Unmarshal([]byte(line), &m) != nil || len(m) != 4 {
				t.Fatal("invalid log")
			}
		}
	}
	if errout.Len() != 0 {
		t.Fatal(errout.String())
	}
}
