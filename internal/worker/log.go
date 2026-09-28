package worker

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sync"

	awprocess "github.com/mtfuller/agentworks/internal/process"
)

type LogSummary struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type outputLog struct {
	mu      sync.Mutex
	file    *os.File
	hash    hash.Hash
	encoder *json.Encoder
	path    string
	bytes   int64
	err     error
}

type outputRecord struct {
	Sequence uint64           `json:"sequence"`
	Stream   awprocess.Stream `json:"stream"`
	Time     string           `json:"time"`
	Data     string           `json:"data_base64"`
}

func openOutputLog(root, runID, attemptID string) (*outputLog, error) {
	runDir := filepath.Dir(LogPath(root, runID, attemptID))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, fmt.Errorf("create run log directory: %w", err)
	}
	path := LogPath(root, runID, attemptID)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create run log: %w", err)
	}
	digest := sha256.New()
	return &outputLog{
		file: file, hash: digest, path: path,
		encoder: json.NewEncoder(&countingWriter{writers: []writer{file, digest}}),
	}, nil
}

// LogPath returns the traversal-safe, opaque path for one persisted attempt log.
func LogPath(root, runID, attemptID string) string {
	return filepath.Join(root, opaqueName(runID), opaqueName(attemptID)+".jsonl")
}

func (log *outputLog) Write(output awprocess.Output) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.err != nil {
		return
	}
	record := outputRecord{
		Sequence: output.Sequence, Stream: output.Stream,
		Time: output.Time.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
		Data: base64.StdEncoding.EncodeToString(output.Data),
	}
	before, _ := log.file.Seek(0, 1)
	if err := log.encoder.Encode(record); err != nil {
		log.err = err
		return
	}
	after, _ := log.file.Seek(0, 1)
	log.bytes += after - before
}

func (log *outputLog) Close() (LogSummary, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.file == nil {
		return LogSummary{}, nil
	}
	if log.err == nil {
		log.err = log.file.Sync()
	}
	if closeErr := log.file.Close(); log.err == nil {
		log.err = closeErr
	}
	log.file = nil
	return LogSummary{
		Path: log.path, SHA256: hex.EncodeToString(log.hash.Sum(nil)), Bytes: log.bytes,
	}, log.err
}

func opaqueName(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:16])
}

type writer interface {
	Write([]byte) (int, error)
}

type countingWriter struct {
	writers []writer
}

func (writer *countingWriter) Write(data []byte) (int, error) {
	for _, target := range writer.writers {
		if _, err := target.Write(data); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}
