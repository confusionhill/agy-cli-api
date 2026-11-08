package agy

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SetUploadsDir configures the directory where uploaded/base64 attachments are stored.
func (r *Runner) SetUploadsDir(dir string) {
	r.uploadsDir = dir
}

// UploadsDir returns the directory used to store uploaded attachments.
func (r *Runner) UploadsDir() string {
	if r.uploadsDir != "" {
		return r.uploadsDir
	}
	return filepath.Join(os.TempDir(), "agy-cli-api-uploads")
}

// SaveBase64Attachment decodes an inline base64 (or data URL) attachment and writes it to UploadsDir.
func (r *Runner) SaveBase64Attachment(att Attachment) (*UploadedFile, error) {
	rawB64 := strings.TrimSpace(att.DataBase64)
	if rawB64 == "" {
		return nil, errors.New("attachment data_base64 is empty")
	}

	mimeType := strings.TrimSpace(att.MimeType)
	// Strip optional data:<mime>;base64, prefix
	if strings.HasPrefix(rawB64, "data:") {
		if commaIdx := strings.Index(rawB64, ","); commaIdx != -1 {
			header := rawB64[5:commaIdx]
			rawB64 = rawB64[commaIdx+1:]
			if semiIdx := strings.Index(header, ";"); semiIdx != -1 && mimeType == "" {
				mimeType = header[:semiIdx]
			}
		}
	}

	decoded, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(rawB64)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 attachment data: %w", err)
		}
	}

	return r.SaveReaderAttachment(att.Filename, mimeType, bytes.NewReader(decoded))
}

// SaveReaderAttachment writes an attachment stream to UploadsDir and returns its metadata.
func (r *Runner) SaveReaderAttachment(filename, mimeType string, reader io.Reader) (*UploadedFile, error) {
	uploadsDir := r.UploadsDir()
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create uploads dir: %w", err)
	}

	// Read first 512 bytes to sniff content type if needed
	head := make([]byte, 512)
	n, err := io.ReadFull(reader, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("read attachment header: %w", err)
	}
	head = head[:n]

	if mimeType == "" && len(head) > 0 {
		mimeType = http.DetectContentType(head)
	}
	if semiIdx := strings.Index(mimeType, ";"); semiIdx != -1 {
		mimeType = strings.TrimSpace(mimeType[:semiIdx])
	}

	safeName := sanitizeFilename(filename, mimeType)
	uniquePrefix := fmt.Sprintf("%d_%s_", time.Now().UnixMilli(), randomHex(4))
	dstPath := filepath.Join(uploadsDir, uniquePrefix+safeName)
	absPath, err := filepath.Abs(dstPath)
	if err != nil {
		absPath = dstPath
	}

	f, err := os.OpenFile(absPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create attachment file: %w", err)
	}
	defer f.Close()

	var totalSize int64
	if len(head) > 0 {
		written, err := f.Write(head)
		if err != nil {
			return nil, fmt.Errorf("write attachment header: %w", err)
		}
		totalSize += int64(written)
	}

	copied, err := io.Copy(f, reader)
	if err != nil {
		return nil, fmt.Errorf("write attachment body: %w", err)
	}
	totalSize += copied

	return &UploadedFile{
		Filename: safeName,
		Path:     absPath,
		URI:      "file://" + filepath.ToSlash(absPath),
		MimeType: mimeType,
		Size:     totalSize,
	}, nil
}

// PrepareChatRequest materializes any inline base64 attachments, adds their directories to AddDirs,
// and injects @/path/to/file references into the prompt so `agy` can read them seamlessly.
func (r *Runner) PrepareChatRequest(req ChatRequest) (ChatRequest, error) {
	var resolvedPaths []string

	for _, p := range req.AttachmentPaths {
		p = strings.TrimSpace(strings.TrimPrefix(p, "file://"))
		if p != "" {
			abs, err := filepath.Abs(p)
			if err == nil {
				p = abs
			}
			resolvedPaths = append(resolvedPaths, p)
		}
	}

	for _, att := range req.Attachments {
		if strings.TrimSpace(att.DataBase64) != "" {
			saved, err := r.SaveBase64Attachment(att)
			if err != nil {
				return req, err
			}
			resolvedPaths = append(resolvedPaths, saved.Path)
		} else if strings.TrimSpace(att.Path) != "" {
			p := strings.TrimSpace(strings.TrimPrefix(att.Path, "file://"))
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
			resolvedPaths = append(resolvedPaths, p)
		}
	}

	if len(resolvedPaths) == 0 {
		return req, nil
	}

	dirSet := make(map[string]bool)
	for _, d := range req.AddDirs {
		dirSet[d] = true
	}

	var missingMentions []string
	for _, p := range resolvedPaths {
		dir := filepath.Dir(p)
		if !dirSet[dir] {
			dirSet[dir] = true
			req.AddDirs = append(req.AddDirs, dir)
		}
		mention := "@" + p
		if !strings.Contains(req.Prompt, p) {
			missingMentions = append(missingMentions, mention)
		}
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = "Please inspect the attached file(s)."
	}
	if len(missingMentions) > 0 {
		prompt = fmt.Sprintf("%s\n\nAttached files:\n%s", prompt, strings.Join(missingMentions, "\n"))
	}
	req.Prompt = prompt

	return req, nil
}

func sanitizeFilename(filename, mimeType string) string {
	base := filepath.Base(strings.TrimSpace(filename))
	if base == "." || base == "/" || base == "" {
		base = "attachment"
	}
	var b strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	cleaned := b.String()
	if filepath.Ext(cleaned) == "" {
		ext := extFromMime(mimeType)
		cleaned += ext
	}
	return cleaned
}

func extFromMime(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "application/pdf":
		return ".pdf"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	}
	if exts, err := mime.ExtensionsByType(mimeType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

func randomHex(nBytes int) string {
	buf := make([]byte, nBytes)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
