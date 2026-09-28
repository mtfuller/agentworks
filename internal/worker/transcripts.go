package worker

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/mtfuller/agentworks/internal/process"
	"github.com/mtfuller/agentworks/internal/store"
)

const maxTranscriptExcerptBytes = 4 << 10

func (worker *Worker) transcriptExcerpts(ctx context.Context, history []store.TimelineEntry) []string {
	excerpts := []string{}
	for index := len(history) - 1; index >= 0 && len(excerpts) < 2; index-- {
		if history[index].Kind != "run" {
			continue
		}
		attempts, err := worker.config.Store.ListAttempts(ctx, history[index].ID)
		if err != nil || len(attempts) == 0 {
			continue
		}
		if excerpt := readTranscriptExcerpt(LogPath(worker.config.LogRoot, history[index].ID, attempts[len(attempts)-1].ID)); excerpt != "" {
			excerpts = append(excerpts, excerpt)
		}
	}
	return excerpts
}

func readTranscriptExcerpt(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var text strings.Builder
	for scanner.Scan() {
		var record outputRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.Stream != process.Stdout {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(record.Data)
		if err != nil {
			continue
		}
		text.Write(data)
	}
	value := strings.TrimSpace(text.String())
	if len(value) > maxTranscriptExcerptBytes {
		start := len(value) - maxTranscriptExcerptBytes
		for start < len(value) && !utf8.RuneStart(value[start]) {
			start++
		}
		value = value[start:]
	}
	return value
}
