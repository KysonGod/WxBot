package application

import (
	"strings"
	"testing"
)

func TestSplitReply_ShortSingleMessage(t *testing.T) {
	in := "你好，今天怎么样？"
	got := splitReply(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 part, got %d: %#v", len(got), got)
	}
	if got[0] != in {
		t.Fatalf("unexpected part: %q", got[0])
	}
}

func TestSplitReply_ExplicitBreaks(t *testing.T) {
	in := "第一句$第二句\\n第三句\n第四句"
	got := splitReply(in)
	want := []string{"第一句", "第二句", "第三句", "第四句"}
	if len(got) != len(want) {
		t.Fatalf("expected %d parts, got %d: %#v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("part %d mismatch, want %q got %q", i, want[i], got[i])
		}
	}
}

func TestSplitReply_LongParagraphAutoSplit(t *testing.T) {
	in := strings.Repeat("这是一段比较长的回复内容，用来测试自动分句能力。", 8)
	got := splitReply(in)
	if len(got) <= 1 {
		t.Fatalf("expected multiple parts for long text, got %#v", got)
	}
	for i, p := range got {
		if strings.TrimSpace(p) == "" {
			t.Fatalf("part %d is empty", i)
		}
		if len([]rune(p)) > hardPartLength {
			t.Fatalf("part %d exceeds hard limit: %d > %d", i, len([]rune(p)), hardPartLength)
		}
	}
}
