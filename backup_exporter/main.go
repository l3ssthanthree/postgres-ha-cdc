package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type backupState struct {
	LastSuccessTimestampSeconds int64  `json:"last_success_timestamp_seconds"`
	LastBackupSizeBytes         int64  `json:"last_backup_size_bytes"`
	LastBackupDurationSeconds   int64  `json:"last_backup_duration_seconds"`
	LastBackupObject            string `json:"last_backup_object"`
}

func main() {
	listenAddress := flag.String("listen-address", ":9091", "HTTP listen address")
	stateFile := flag.String("state-file", "/var/lib/backup/state/state.json", "Path to the backup state file")
	flag.Parse()

	http.HandleFunc("/metrics", metricsHandler(*stateFile))
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	log.Printf("backup exporter listening on %s", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}

func metricsHandler(stateFile string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		state, err := loadState(stateFile)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to read state: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, renderMetrics(state))
	}
}

func loadState(path string) (backupState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return backupState{}, nil
		}
		return backupState{}, err
	}

	var state backupState
	if err := json.Unmarshal(data, &state); err != nil {
		return backupState{}, err
	}

	return state, nil
}

func renderMetrics(state backupState) string {
	var builder strings.Builder

	builder.WriteString("# HELP backup_last_success_timestamp_seconds Unix timestamp of the most recent successful backup.\n")
	builder.WriteString("# TYPE backup_last_success_timestamp_seconds gauge\n")
	builder.WriteString("backup_last_success_timestamp_seconds ")
	builder.WriteString(strconv.FormatInt(state.LastSuccessTimestampSeconds, 10))
	builder.WriteString("\n")

	builder.WriteString("# HELP backup_last_size_bytes Size of the most recent successful backup in bytes.\n")
	builder.WriteString("# TYPE backup_last_size_bytes gauge\n")
	builder.WriteString("backup_last_size_bytes ")
	builder.WriteString(strconv.FormatInt(state.LastBackupSizeBytes, 10))
	builder.WriteString("\n")

	builder.WriteString("# HELP backup_last_duration_seconds Duration of the most recent successful backup in seconds.\n")
	builder.WriteString("# TYPE backup_last_duration_seconds gauge\n")
	builder.WriteString("backup_last_duration_seconds ")
	builder.WriteString(strconv.FormatInt(state.LastBackupDurationSeconds, 10))
	builder.WriteString("\n")

	if state.LastBackupObject != "" {
		builder.WriteString("# HELP backup_last_success_info Metadata about the most recent successful backup.\n")
		builder.WriteString("# TYPE backup_last_success_info gauge\n")
		builder.WriteString("backup_last_success_info{object=\"")
		builder.WriteString(escapeLabelValue(state.LastBackupObject))
		builder.WriteString("\"} 1\n")
	}

	return builder.String()
}

func escapeLabelValue(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n")
	return replacer.Replace(value)
}
