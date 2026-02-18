package application

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShouldCleanupMediaPath_AllowsTempImage(t *testing.T) {
	p := filepath.Join(os.TempDir(), "wxbot_test_image.png")
	if !shouldCleanupMediaPath(p) {
		t.Fatalf("expected temp image path to be cleanable: %s", p)
	}
}

func TestShouldCleanupMediaPath_RejectsNonImage(t *testing.T) {
	p := filepath.Join(os.TempDir(), "wxbot_test_file.txt")
	if shouldCleanupMediaPath(p) {
		t.Fatalf("expected non-image path to be rejected: %s", p)
	}
}
