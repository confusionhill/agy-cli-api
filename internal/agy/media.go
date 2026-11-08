package agy

import (
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MediaItem represents an extracted media or document asset.
type MediaItem struct {
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	FilePath  string `json:"file_path,omitempty"`
	MediaURL  string `json:"media_url"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

var mediaPathRegex = regexp.MustCompile(`(?:file://)?(/[^\s"'` + "`" + `\(\)]+\.(?:jpg|jpeg|png|webp|gif|svg|pdf|mp3|mp4|wav))`)

// ExtractMediaFromText scans a response string for generated or referenced local media files.
func ExtractMediaFromText(text string) []MediaItem {
	matches := mediaPathRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var items []MediaItem

	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		cleanPath := strings.TrimSpace(m[1])
		cleanPath = strings.TrimPrefix(cleanPath, "file://")
		if unescaped, err := url.PathUnescape(cleanPath); err == nil {
			cleanPath = unescaped
		}
		cleanPath = filepath.Clean(cleanPath)

		if seen[cleanPath] {
			continue
		}
		seen[cleanPath] = true

		// Check if file actually exists on disk
		fi, err := os.Stat(cleanPath)
		if err != nil {
			continue
		}

		ext := strings.ToLower(filepath.Ext(cleanPath))
		mimeType := mime.TypeByExtension(ext)
		if mimeType == "" {
			switch ext {
			case ".jpg", ".jpeg":
				mimeType = "image/jpeg"
			case ".png":
				mimeType = "image/png"
			case ".webp":
				mimeType = "image/webp"
			case ".pdf":
				mimeType = "application/pdf"
			default:
				mimeType = "application/octet-stream"
			}
		}

		items = append(items, MediaItem{
			Filename:  filepath.Base(cleanPath),
			MimeType:  mimeType,
			FilePath:  cleanPath,
			MediaURL:  "/api/v1/media?uri=" + url.QueryEscape(cleanPath),
			SizeBytes: fi.Size(),
		})
	}

	return items
}
