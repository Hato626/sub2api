package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const bpsImageMaxBytes = 32 << 20
const bpsImageMaxPixels = 100_000_000

var bpsImageNamePattern = regexp.MustCompile("^[a-f0-9]{64}\\.(png|jpg|webp|gif)$")

type BPSImageStore struct{ root string }

func NewBPSImageStore(root string) *BPSImageStore {
	if strings.TrimSpace(root) == "" {
		root = filepath.Join("data", "bps-images")
	}
	return &BPSImageStore{root: filepath.Clean(root)}
}
func newBPSImageStoreFromConfig(cfg *config.Config) *BPSImageStore {
	if cfg != nil && strings.TrimSpace(cfg.BPSImages.DataDir) != "" {
		return NewBPSImageStore(cfg.BPSImages.DataDir)
	}
	if d := strings.TrimSpace(os.Getenv("DATA_DIR")); d != "" {
		return NewBPSImageStore(filepath.Join(d, "bps-images"))
	}
	return NewBPSImageStore("")
}
func decodeBPSImage(raw string) ([]byte, string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 5 || !strings.EqualFold(raw[:5], "data:") {
		return nil, "", errors.New("BPS image must be a base64 data URL")
	}
	h, p, ok := strings.Cut(raw[5:], ",")
	if !ok || len(h) > 64 {
		return nil, "", errors.New("invalid BPS image data URL")
	}
	parts := strings.Split(strings.ToLower(h), ";")
	if len(parts) != 2 || parts[1] != "base64" {
		return nil, "", errors.New("BPS image must be base64 encoded")
	}
	mime := parts[0]
	if mime == "image/jpg" {
		mime = "image/jpeg"
	}
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return nil, "", errors.New("BPS images support PNG, JPEG, GIF and WebP only")
	}
	if len(p) > base64.StdEncoding.EncodedLen(bpsImageMaxBytes)+4096 {
		return nil, "", errors.New("BPS image exceeds 32 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(base64.NewDecoder(base64.StdEncoding, strings.NewReader(p)), bpsImageMaxBytes+1))
	if err != nil || len(data) == 0 || len(data) > bpsImageMaxBytes {
		return nil, "", errors.New("invalid or oversized BPS image")
	}
	d, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || d.Width <= 0 || d.Height <= 0 || int64(d.Width) > bpsImageMaxPixels/int64(d.Height) {
		return nil, "", errors.New("invalid BPS image dimensions")
	}
	if "image/"+format != mime {
		return nil, "", errors.New("BPS image MIME mismatch")
	}
	return data, mime, nil
}
func (s *BPSImageStore) PutDataURL(raw string) (string, error) {
	data, ct, err := decodeBPSImage(raw)
	if err != nil {
		return "", err
	}
	return s.put(data, ct)
}
func (s *BPSImageStore) put(data []byte, ct string) (string, error) {
	name := fmt.Sprintf("%x", sha256.Sum256(data)) + bpsImageExtension(ct)
	if err := os.MkdirAll(s.root, 0750); err != nil {
		return "", err
	}
	target := filepath.Join(s.root, name)
	if st, err := os.Stat(target); err == nil && st.Mode().IsRegular() && st.Size() == int64(len(data)) {
		return name, nil
	}
	tmp, err := os.CreateTemp(s.root, ".upload-*")
	if err != nil {
		return "", err
	}
	n := tmp.Name()
	defer func() { _ = tmp.Close(); _ = os.Remove(n) }()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if err == nil {
		err = tmp.Close()
	}
	if err != nil {
		return "", err
	}
	if err = os.Link(n, target); err != nil {
		if _, e := os.Stat(target); e != nil {
			return "", err
		}
	}
	return name, nil
}
func (s *BPSImageStore) Open(name string) (*os.File, string, error) {
	if s == nil || !bpsImageNamePattern.MatchString(name) {
		return nil, "", os.ErrNotExist
	}
	p := filepath.Join(s.root, name)
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > bpsImageMaxBytes {
		return nil, "", os.ErrNotExist
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, "", err
	}
	return f, bpsImageContentType(name), nil
}
func bpsImageExtension(ct string) string {
	switch ct {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}
func bpsImageContentType(name string) string {
	switch filepath.Ext(name) {
	case ".jpg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/png"
	}
}
