package application

import (
	"context"
	"regexp"
	"strings"
	"time"
)

var timestampPattern = regexp.MustCompile(`\[\d{4}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12]\d|3[01])(?:\s[A-Za-z]+)?\s(?:2[0-3]|[01]\d):[0-5]\d(?::[0-5]\d)?\]`)

var sentenceEnders = map[rune]bool{
	'。': true,
	'！': true,
	'？': true,
	'!': true,
	'?': true,
	'；': true,
	';': true,
}

var commaDelimiters = map[rune]bool{
	'，': true,
	',': true,
	'、': true,
	'：': true,
	':': true,
}

const (
	singleMessageThreshold = 120
	targetPartLength       = 110
	hardPartLength         = 160
)

func splitReply(text string) []string {
	cleaned := strings.TrimSpace(removeTimestamps(text))
	if cleaned == "" {
		return nil
	}
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\n")
	cleaned = strings.ReplaceAll(cleaned, "\r", "\n")
	cleaned = strings.ReplaceAll(cleaned, "\\n", "\n")

	sections := splitByExplicitBreaks(cleaned)
	if len(sections) == 0 {
		return nil
	}
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		section = normalizeSpaces(section)
		if section == "" {
			continue
		}
		if runeLen(section) <= singleMessageThreshold {
			parts = append(parts, section)
			continue
		}
		parts = append(parts, splitLongSection(section)...)
	}
	return parts
}

func removeTimestamps(text string) string {
	return timestampPattern.ReplaceAllString(text, " ")
}

func (s *Service) sendReplyWithDelay(ctx context.Context, chatID, reply string) {
	parts := splitReply(reply)
	if len(parts) == 0 {
		s.logFlow(chatID, "reply skipped because split result is empty")
		return
	}
	s.logFlow(chatID, "reply split parts=%d mode=queued", len(parts))
	for idx, part := range parts {
		s.logFlow(chatID, "reply part_index=%d/%d content=%s", idx+1, len(parts), textPreview(part, logReplyPreviewMax))
		if err := s.wx.SendText(ctx, chatID, part); err != nil {
			s.logger.Printf("send reply failed to %s: %v", chatID, err)
			return
		}
		if idx == len(parts)-1 {
			continue
		}
		delay := perPartDelay(part)
		s.clock.Sleep(delay)
	}
	s.logFlow(chatID, "reply send done parts=%d mode=queued", len(parts))
}

func perPartDelay(part string) time.Duration {
	r := []rune(part)
	ms := 450 + len(r)*22
	if ms < 700 {
		ms = 700
	}
	if ms > 2800 {
		ms = 2800
	}
	return time.Duration(ms) * time.Millisecond
}

func splitByExplicitBreaks(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	parts := make([]string, 0, 8)
	var b strings.Builder
	flush := func() {
		p := strings.TrimSpace(b.String())
		if p != "" {
			parts = append(parts, p)
		}
		b.Reset()
	}
	for _, r := range text {
		if r == '$' || r == '\n' {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return parts
}

func normalizeSpaces(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		out = append(out, strings.Join(fields, " "))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func splitLongSection(section string) []string {
	if runeLen(section) <= singleMessageThreshold {
		return []string{section}
	}

	sentences := splitByDelimitersKeeping(section, sentenceEnders)
	if len(sentences) <= 1 {
		return splitByCommaThenHard(section)
	}

	parts := make([]string, 0, len(sentences))
	var current strings.Builder
	currentLen := 0
	flush := func() {
		p := strings.TrimSpace(current.String())
		if p != "" {
			parts = append(parts, p)
		}
		current.Reset()
		currentLen = 0
	}

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		sLen := runeLen(sentence)
		if sLen > hardPartLength {
			flush()
			parts = append(parts, splitByCommaThenHard(sentence)...)
			continue
		}
		if currentLen == 0 {
			current.WriteString(sentence)
			currentLen = sLen
			continue
		}
		if currentLen+sLen <= targetPartLength {
			current.WriteString(sentence)
			currentLen += sLen
			continue
		}
		flush()
		current.WriteString(sentence)
		currentLen = sLen
	}
	flush()

	if len(parts) == 0 {
		return splitByCommaThenHard(section)
	}
	return parts
}

func splitByCommaThenHard(section string) []string {
	units := splitByDelimitersKeeping(section, commaDelimiters)
	if len(units) == 0 {
		return hardChunk(section, hardPartLength)
	}

	parts := make([]string, 0, len(units))
	var current strings.Builder
	currentLen := 0
	flush := func() {
		p := strings.TrimSpace(current.String())
		if p != "" {
			parts = append(parts, p)
		}
		current.Reset()
		currentLen = 0
	}

	for _, unit := range units {
		unit = strings.TrimSpace(unit)
		if unit == "" {
			continue
		}
		uLen := runeLen(unit)
		if uLen > hardPartLength {
			flush()
			parts = append(parts, hardChunk(unit, hardPartLength)...)
			continue
		}
		if currentLen == 0 {
			current.WriteString(unit)
			currentLen = uLen
			continue
		}
		if currentLen+uLen <= targetPartLength {
			current.WriteString(unit)
			currentLen += uLen
			continue
		}
		flush()
		current.WriteString(unit)
		currentLen = uLen
	}
	flush()

	if len(parts) == 0 {
		return hardChunk(section, hardPartLength)
	}
	return parts
}

func splitByDelimitersKeeping(text string, delimiters map[rune]bool) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	parts := make([]string, 0, 8)
	var b strings.Builder
	flush := func() {
		p := strings.TrimSpace(b.String())
		if p != "" {
			parts = append(parts, p)
		}
		b.Reset()
	}
	for _, r := range text {
		b.WriteRune(r)
		if delimiters[r] {
			flush()
		}
	}
	flush()
	return parts
}

func hardChunk(text string, maxLen int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxLen <= 0 {
		return []string{text}
	}
	r := []rune(text)
	if len(r) <= maxLen {
		return []string{text}
	}
	parts := make([]string, 0, len(r)/maxLen+1)
	for start := 0; start < len(r); start += maxLen {
		end := start + maxLen
		if end > len(r) {
			end = len(r)
		}
		p := strings.TrimSpace(string(r[start:end]))
		if p == "" {
			continue
		}
		parts = append(parts, p)
	}
	return parts
}

func runeLen(text string) int {
	return len([]rune(text))
}
