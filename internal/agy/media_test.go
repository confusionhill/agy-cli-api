package agy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractMediaFromText(t *testing.T) {
	// Create a temporary file to simulate generated media
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "sample_artifact.jpg")
	if err := os.WriteFile(imgPath, []byte("fake-image-bytes"), 0644); err != nil {
		t.Fatalf("failed to create sample file: %v", err)
	}

	text := "Here is your generated image:\n\n![" + imgPath + "](" + imgPath + ")\n\nSaved to: [sample_artifact.jpg](file://" + imgPath + ")"
	items := ExtractMediaFromText(text)

	if len(items) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(items))
	}

	if items[0].Filename != "sample_artifact.jpg" {
		t.Errorf("expected filename sample_artifact.jpg, got %s", items[0].Filename)
	}
	if items[0].MimeType != "image/jpeg" {
		t.Errorf("expected mime_type image/jpeg, got %s", items[0].MimeType)
	}
	if items[0].FilePath != imgPath {
		t.Errorf("expected FilePath %s, got %s", imgPath, items[0].FilePath)
	}
	if items[0].SizeBytes != int64(len("fake-image-bytes")) {
		t.Errorf("expected SizeBytes %d, got %d", len("fake-image-bytes"), items[0].SizeBytes)
	}
}

