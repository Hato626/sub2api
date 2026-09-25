package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const bpsImagePublicPath = "/api/v1/bps/images/"

// Only visit Responses message content and structured tool output. Never rewrite
// tool schemas, arguments, encrypted content or text that happens to contain JSON.
func (s *OpenAIGatewayService) materializeBPSInlineImages(c *gin.Context, body []byte) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	var request map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&request); err != nil {
		return nil, errors.New("invalid BPS request JSON")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid BPS request JSON")
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	type pendingImage struct {
		part      map[string]any
		raw       string
		normalize bool
		detail    string
	}
	images := make([]pendingImage, 0)
	var collect func(any)
	collect = func(node any) {
		switch value := node.(type) {
		case []any:
			for _, child := range value {
				collect(child)
			}
		case map[string]any:
			typ, _ := value["type"].(string)
			entry := pendingImage{part: value}
			switch strings.ToLower(strings.TrimSpace(typ)) {
			case "input_image", "image_url":
				if raw, ok := value["image_url"].(string); ok {
					entry.raw = raw
				}
				if object, ok := value["image_url"].(map[string]any); ok {
					entry.raw, _ = object["url"].(string)
					entry.detail, _ = object["detail"].(string)
				}
				entry.normalize = true
			case "image":
				if source, ok := value["source"].(map[string]any); ok {
					sourceType, _ := source["type"].(string)
					if sourceType == "base64" {
						mime, _ := source["media_type"].(string)
						data, _ := source["data"].(string)
						entry.raw = "data:" + mime + ";base64," + data
					} else if sourceType == "url" {
						entry.raw, _ = source["url"].(string)
					}
				}
				if entry.raw == "" {
					mime, _ := value["mimeType"].(string)
					data, _ := value["data"].(string)
					if mime != "" && data != "" {
						entry.raw = "data:" + mime + ";base64," + data
					}
				}
				entry.normalize = true
			}
			entry.raw = strings.TrimSpace(entry.raw)
			if entry.raw != "" && (strings.HasPrefix(strings.ToLower(entry.raw), "data:") || entry.normalize) {
				images = append(images, entry)
			}
			for _, child := range value {
				collect(child)
			}
		}
	}
	collect(request)
	if len(images) == 0 {
		return body, nil
	} // Text-only BPS requires no image-host config.
	if len(images) > 256 {
		return nil, errors.New("BPS request contains too many image parts (maximum 256)")
	}
	type preparedImage struct {
		data []byte
		mime string
		name string
	}
	prepared := map[string]*preparedImage{}
	total := 0
	needsStorage := false
	for _, entry := range images {
		if !strings.HasPrefix(strings.ToLower(entry.raw), "data:") {
			continue
		}
		needsStorage = true
		if prepared[entry.raw] != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, mime, err := decodeBPSImage(entry.raw)
		if err != nil {
			return nil, err
		}
		total += len(data)
		if total > 64<<20 {
			return nil, errors.New("BPS inline images exceed 64 MiB per request")
		}
		prepared[entry.raw] = &preparedImage{data: data, mime: mime}
	}
	base := ""
	if needsStorage {
		if s == nil || s.bpsImageStore == nil {
			return nil, errors.New("BPS image store is unavailable")
		}
		var err error
		base, err = s.bpsImagePublicBaseURL(c)
		if err != nil {
			return nil, err
		}
	}
	for _, entry := range images {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		imageURL := entry.raw
		if stored := prepared[entry.raw]; stored != nil {
			if stored.name == "" {
				name, err := s.bpsImageStore.put(stored.data, stored.mime)
				if err != nil {
					return nil, errors.New("BPS image storage write failed; check the server data directory")
				}
				stored.name = name
			}
			imageURL = base + bpsImagePublicPath + stored.name
		}
		entry.part["type"] = "input_image"
		entry.part["image_url"] = imageURL
		delete(entry.part, "source")
		delete(entry.part, "data")
		delete(entry.part, "mimeType")
		if entry.detail != "" {
			if _, ok := entry.part["detail"]; !ok {
				entry.part["detail"] = entry.detail
			}
		}
	}
	return json.Marshal(request)
}

func (s *OpenAIGatewayService) bpsImagePublicBaseURL(c *gin.Context) (string, error) {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	base := ""
	if s != nil && s.cfg != nil {
		base = strings.TrimSpace(s.cfg.BPSImages.PublicBaseURL)
	}
	if base == "" && s != nil && s.settingService != nil {
		base = s.settingService.GetAPIBaseURL(ctx)
	}
	if base == "" && s != nil && s.cfg != nil {
		base = strings.TrimSpace(s.cfg.Server.FrontendURL)
	}
	// Never derive a public link from untrusted Host/X-Forwarded-* headers.
	parsed, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", errors.New("BPS image conversion requires a public HTTP(S) URL: configure bps_images.public_base_url or the admin API base URL")
	}
	for _, suffix := range []string{"/api/v1", "/v1", "/api"} {
		if strings.HasSuffix(parsed.Path, suffix) {
			parsed.Path = strings.TrimSuffix(parsed.Path, suffix)
			break
		}
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (s *OpenAIGatewayService) ServeBPSImage(c *gin.Context) {
	if s == nil || s.bpsImageStore == nil {
		c.Status(http.StatusNotFound)
		return
	}
	name := c.Param("name")
	file, contentType, err := s.bpsImageStore.Open(name)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", contentType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("X-Robots-Tag", "noindex, nofollow, noarchive")
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", fmt.Sprintf("%q", name))
	http.ServeContent(c.Writer, c.Request, name, info.ModTime(), file)
}
