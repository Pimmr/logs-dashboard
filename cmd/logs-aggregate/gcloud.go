package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"time"
)

func gcloudStream(conf Config, logName string) io.ReadCloser {
	r, w := io.Pipe()
	enc := json.NewEncoder(w)

	go func() {
		var lastTimestamp time.Time

		interval := conf.GcloudPoll
		if !conf.Follow {
			interval = 0
		}

		knownInsertIDs := make(map[string]struct{}, 10000)

		for range Tick(interval) {
			entries, err := gcloudStreamEntries(conf, lastTimestamp, logName)
			if err != nil {
				_ = enc.Encode(json.RawMessage([]byte("Error: " + err.Error())))
				return
			}

			// entries is in reverse chronological order
			for i := len(entries) - 1; i >= 0; i-- {
				if _, ok := knownInsertIDs[entries[i].InsertID]; ok {
					continue
				}
				knownInsertIDs[entries[i].InsertID] = struct{}{}

				entry := entries[i].ToLogrus()
				err := enc.Encode(entry)
				if err != nil {
					_ = enc.Encode(json.RawMessage([]byte("Error: failed to encode log")))
				}
			}
			if len(entries) != 0 {
				lastTimestamp = entries[0].Timestamp
			}
			if !conf.Follow {
				w.Close()
				return
			}
		}
	}()

	return r
}

func Tick(d time.Duration) <-chan time.Time {
	c := make(chan time.Time)

	if d == 0 {
		go func() {
			c <- time.Now()
			close(c)
		}()

		return c
	}

	go func() {
		c <- time.Now()

		for t := range time.Tick(d) {
			c <- t
		}
	}()

	return c
}

func gcloudStreamEntries(conf Config, lastTimestamp time.Time, logName string) ([]Entry, error) {
	cmdName, args := gcloudStreamBuildCmd(conf, lastTimestamp, logName)

	cmd := exec.Command(cmdName, args...)
	out := &bytes.Buffer{}
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	var entries []Entry

	err = json.NewDecoder(out).Decode(&entries)
	if err != nil {
		return nil, err
	}

	return entries, nil
}

func gcloudStreamBuildCmd(conf Config, lastTimestamp time.Time, logName string) (string, []string) {
	logPath := path.Join("projects", conf.GcloudProject, "logs", logName)
	filter := "logName=" + logPath

	if !lastTimestamp.IsZero() {
		filter += " timestamp>=" + strconv.Quote(lastTimestamp.UTC().Format(time.RFC3339))
	}

	args := []string{
		"logging", "read",
		filter,
		"--format=json",
	}
	if lastTimestamp.IsZero() && conf.Since > 0 {
		args = append(args, "--freshness="+conf.Since.String())
	}
	if conf.Tail >= 0 {
		args = append(args, "--limit="+strconv.FormatInt(conf.Tail, 10))
	}

	return "gcloud", args
}

type Entry struct {
	JSONPayload json.RawMessage
	Severity    string
	Timestamp   time.Time
	InsertID    string

	msg     string
	payload map[string]interface{}
}

var severityMap = map[string]string{
	"INFO":  "info",
	"ERROR": "error",
}

func (e *Entry) ToLogrus() map[string]interface{} {
	payload := e.Payload()
	logrusEntry := make(map[string]interface{}, len(payload)+3)

	// TODO: flatten .Exception
	for k, v := range payload {
		switch k {
		case "msg", "time", "level":
			k = "entry." + k
		}

		logrusEntry[k] = v
	}

	logrusEntry["time"] = e.Timestamp
	logrusEntry["msg"] = e.Message()
	if level, ok := severityMap[e.Severity]; ok {
		logrusEntry["level"] = level
	} else {
		logrusEntry["level"] = "panic"
	}

	return logrusEntry
}

func (e *Entry) Payload() map[string]interface{} {
	if e.payload != nil {
		return e.payload
	}

	err := json.Unmarshal(e.JSONPayload, &e.payload)
	if err != nil {
		e.payload = map[string]interface{}{
			"log_bytes": []byte(e.JSONPayload),
			"log_error": err,
		}
	}

	return e.payload
}

func (e *Entry) Message() string {
	if e.msg != "" {
		return e.msg
	}

	var payload struct {
		Message   string
		Exception struct {
			Message string
		}
	}

	err := json.Unmarshal(e.JSONPayload, &payload)
	if err != nil {
		e.msg = "-"
		return e.msg
	}
	if payload.Message == "" && payload.Exception.Message == "" {
		e.msg = "-"
		return e.msg
	}
	if payload.Message == "" && payload.Exception.Message != "" {
		e.msg = payload.Exception.Message
		return e.msg
	}

	e.msg = payload.Message
	return e.msg
}
