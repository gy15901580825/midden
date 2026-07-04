package claude

import (
	"bufio"
	"encoding/json"
	"io"
	"midden/internal/adapter"
	"os"
	"strings"
)

// readCwd returns the first non-empty "cwd" field found in the
// first 50 lines of a session jsonl, or "".
func readCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	for i := 0; i < 50; i++ {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var rec struct {
				Cwd string `json:"cwd"`
			}
			if json.Unmarshal(line, &rec) == nil && rec.Cwd != "" {
				return rec.Cwd
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return ""
		}
	}
	return ""
}

type sessionMeta struct {
	Title        string
	Records      int
	Messages     int
	HasAssistant bool
}

func parseMeta(path string) sessionMeta {
	var m sessionMeta
	var fallback string
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 1 {
			var rec struct {
				Type    string `json:"type"`
				AiTitle string `json:"aiTitle"`
				Message struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &rec) == nil && rec.Type != "" {
				m.Records++
				switch rec.Type {
				case "user":
					m.Messages++
					if fallback == "" {
						fallback = textOf(rec.Message.Content)
					}
				case "assistant":
					m.Messages++
					m.HasAssistant = true
				case "ai-title":
					m.Title = rec.AiTitle
				}
			}
		}
		if err != nil {
			break
		}
	}
	if m.Title == "" {
		m.Title = fallback
	}
	m.Title = cleanTitle(m.Title)
	return m
}

// textOf extracts plain text from a message content that is either a
// JSON string or an array of {type,text} blocks.
func textOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var arr []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &arr) == nil {
		for _, el := range arr {
			if el.Type == "text" && strings.TrimSpace(el.Text) != "" {
				return el.Text
			}
		}
	}
	return ""
}

func cleanTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

// readMessages returns all non-empty user/assistant text messages.
func readMessages(path string) []adapter.Message {
	var out []adapter.Message
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 1 {
			var rec struct {
				Type    string `json:"type"`
				Message struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &rec) == nil &&
				(rec.Type == "user" || rec.Type == "assistant") {
				if txt := cleanTitle(textOf(rec.Message.Content)); txt != "" {
					out = append(out, adapter.Message{Role: rec.Type, Text: txt})
				}
			}
		}
		if err != nil {
			break
		}
	}
	return out
}
