package domain

import (
	"math/rand"
	"strings"
)

type GroupPolicy struct {
	AcceptAll           bool
	EnableAtReply       bool
	EnableKeywordReply  bool
	Keywords            []string
	ResponseProbability int
	RobotName           string
}

func ShouldProcess(msg IncomingMessage, policy GroupPolicy) (bool, string) {
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return false, ""
	}

	if !msg.IsGroup {
		if strings.EqualFold(strings.TrimSpace(msg.Attr), "friend") {
			return true, content
		}
		return false, ""
	}

	atTriggered := false
	keywordTriggered := false
	processed := content

	if policy.EnableAtReply && strings.TrimSpace(policy.RobotName) != "" {
		robot := strings.TrimSpace(policy.RobotName)
		patterns := []string{"@" + robot + "\u2005", "@" + robot + " "}
		for _, p := range patterns {
			if strings.Contains(processed, p) {
				processed = strings.TrimSpace(strings.Replace(processed, p, "", 1))
				atTriggered = true
				break
			}
		}
		if !atTriggered && strings.TrimSpace(content) == "@"+robot {
			atTriggered = true
			processed = ""
		}
	}

	if policy.EnableKeywordReply && len(policy.Keywords) > 0 {
		for _, kw := range policy.Keywords {
			if kw == "" {
				continue
			}
			if strings.Contains(processed, kw) {
				keywordTriggered = true
				break
			}
		}
	}

	basic := policy.AcceptAll || atTriggered || keywordTriggered
	if !basic {
		return false, ""
	}

	p := policy.ResponseProbability
	if p <= 0 || p > 100 {
		p = 100
	}
	if rand.Intn(100)+1 > p {
		return false, ""
	}

	if processed == "" && atTriggered {
		processed = "@bot"
	}
	return true, processed
}
