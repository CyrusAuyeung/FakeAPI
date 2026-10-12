package cli

import (
	"io"
	"log"
)

type fixedWriter struct{ out io.Writer }

func (w fixedWriter) Write(b []byte) (int, error) {
	_, e := io.WriteString(w.out, "E_HTTP_INTERNAL\n")
	return len(b), e
}
func fixedLog(out io.Writer) *log.Logger { return log.New(fixedWriter{out}, "", 0) }
