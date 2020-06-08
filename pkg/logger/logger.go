package logger

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// StandardLogger represents a log function that can optionally send messages
// to SumoLogic
type StandardLogger struct {
	logToSumo bool
	sumoURL   string
	sink      io.Writer
	hc        http.Client
}

// NewStandardLogger returns a StandardLogger object
// If sumoURL is not an empty string log entries will be send to SumoLogic as
// well as the sink
func NewStandardLogger(sink io.Writer, sumoURL string) *StandardLogger {
	return &StandardLogger{
		logToSumo: sumoURL != "",
		sumoURL:   sumoURL,
		sink:      sink,
		hc:        http.Client{},
	}
}

// Write makes StandardLogger satisfy io.Writer so it can be used with stdlib
// Log package
func (sl *StandardLogger) Write(data []byte) (int, error) {
	if sl.logToSumo {
		go sl._sumo(data)
	}
	return sl.sink.Write(data)
}

func (sl *StandardLogger) _sumo(data []byte) {
	body := bytes.NewBuffer(nil)
	body.Write(data)
	req, err := http.NewRequest("POST", sl.sumoURL, body)
	if err != nil {
		sl.sink.Write([]byte(fmt.Sprintf("error creating sumologic request: %v", err)))
		return
	}
	resp, err := sl.hc.Do(req)
	if err != nil || resp.StatusCode > 299 {
		sl.sink.Write([]byte(fmt.Sprintf("error performing sumologic request: %v", err)))
	}
}
