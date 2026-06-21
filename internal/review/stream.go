package review

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StreamOutcome is what we extract from a claude --output-format stream-json run.
type StreamOutcome struct {
	Text       string   // the final result text
	SkillsUsed []string // skills invoked via the Skill tool, in order
	Subagents  int      // number of Task (subagent) tool_uses
	IsError    bool     // the terminal result reported an error
	ErrMsg     string   // result text when IsError
}

// parseStream consumes newline-delimited stream-json events and summarizes the
// run: final text, which skills were invoked (proof a review skill actually
// ran), and how many subagents it dispatched. Unknown/garbage lines are skipped.
func parseStream(raw []byte) (StreamOutcome, error) {
	var out StreamOutcome
	sawResult := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			IsError bool   `json:"is_error"`
			Result  string `json:"result"`
			Message struct {
				Content []struct {
					Type  string          `json:"type"`
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // not a JSON event line; ignore
		}
		switch ev.Type {
		case "assistant":
			for _, b := range ev.Message.Content {
				if b.Type != "tool_use" {
					continue
				}
				switch b.Name {
				case "Skill":
					if s := skillName(b.Input); s != "" {
						out.SkillsUsed = append(out.SkillsUsed, s)
					}
				case "Task":
					out.Subagents++
				}
			}
		case "result":
			sawResult = true
			out.IsError = ev.IsError
			if ev.IsError {
				out.ErrMsg = ev.Result
			} else {
				out.Text = ev.Result
			}
		}
	}
	if !sawResult {
		return out, fmt.Errorf("no result event in claude stream")
	}
	return out, nil
}

func skillName(input json.RawMessage) string {
	var in struct {
		Skill   string `json:"skill"`
		Command string `json:"command"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ""
	}
	switch {
	case in.Skill != "":
		return in.Skill
	case in.Command != "":
		return in.Command
	default:
		return in.Name
	}
}
