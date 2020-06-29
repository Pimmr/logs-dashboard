package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strconv"
	"time"
)

func gcloudStream(conf Config, logName string) (io.ReadCloser, error) {
	logPath := path.Join("projects", conf.GcloudProject, "logs", logName)
	filter := "logName=" + logPath

	args := []string{
		"logging", "read",
		filter,
		"--format=json",
	}
	if conf.Since > 0 {
		args = append(args, "--freshness="+conf.Since.String())
	}
	if conf.Tail >= 0 {
		args = append(args, "--limit="+strconv.FormatInt(conf.Tail, 10))
	}

	cmd := exec.Command("gcloud", args...)
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

	ret := &bytes.Buffer{}
	enc := json.NewEncoder(ret)
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i].ToLogrus()
		err := enc.Encode(entry)
		if err != nil {
			_ = enc.Encode(json.RawMessage([]byte("Error: failed to encode log")))
		}
	}

	return ioutil.NopCloser(ret), nil
}

type Entry struct {
	JSONPayload json.RawMessage
	Severity    string
	Timestamp   time.Time

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
