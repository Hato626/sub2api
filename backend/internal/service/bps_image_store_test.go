package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func bpsTestPNG(t *testing.T) ([]byte, string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes(), "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}
func bpsTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "http://untrusted.example/responses", nil)
	return c
}
func bpsTestGateway(t *testing.T) *OpenAIGatewayService {
	t.Helper()
	return &OpenAIGatewayService{bpsImageStore: NewBPSImageStore(t.TempDir()), cfg: &config.Config{BPSImages: config.BPSImageConfig{PublicBaseURL: "http://192.0.2.1:8080"}}}
}
func TestBPSImageStoreDeduplicatesAcrossRequestsAndRestarts(t *testing.T) {
	data, raw := bpsTestPNG(t)
	root := t.TempDir()
	store := NewBPSImageStore(root)
	first, err := store.PutDataURL(raw)
	require.NoError(t, err)
	fileInfo, err := os.Stat(filepath.Join(root, first))
	require.NoError(t, err)
	// New service object and different base64 formatting still reuse the same bytes.
	payload := strings.SplitN(raw, ",", 2)[1]
	second, err := NewBPSImageStore(root).PutDataURL("data:image/png;base64," + payload[:12] + "\r\n" + payload[12:])
	require.NoError(t, err)
	require.Equal(t, first, second)
	after, err := os.Stat(filepath.Join(root, first))
	require.NoError(t, err)
	require.Equal(t, fileInfo.ModTime(), after.ModTime())
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	f, ct, err := store.Open(first)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	stored, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, data, stored)
	require.Equal(t, "image/png", ct)
}
func TestBPSImageStoreConcurrentDedupe(t *testing.T) {
	data, raw := bpsTestPNG(t)
	root := t.TempDir()
	var wg sync.WaitGroup
	names := make(chan string, 24)
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); n, e := NewBPSImageStore(root).PutDataURL(raw); names <- n; errs <- e }()
	}
	wg.Wait()
	close(names)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	unique := map[string]bool{}
	for name := range names {
		unique[name] = true
	}
	require.Len(t, unique, 1)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	stored, err := os.ReadFile(filepath.Join(root, entries[0].Name()))
	require.NoError(t, err)
	require.Equal(t, data, stored)
}
func TestBPSImageStoreRejectsInvalidDataAndTraversal(t *testing.T) {
	store := NewBPSImageStore(t.TempDir())
	_, pngData := bpsTestPNG(t)
	for _, raw := range []string{"data:", "x", "data:image/png;base64,%%%", "data:image/png;base64,", "data:image/png;base64,cG5nLWJ5dGVz", "data:image/svg+xml;base64,PHN2Zz4=", strings.Replace(pngData, "image/png", "image/jpeg", 1), "data:text/html;base64,PGgxPkhlbGxvPC9oMT4="} {
		_, err := store.PutDataURL(raw)
		require.Error(t, err)
	}
	_, err := store.PutDataURL("data:image/png;base64," + strings.Repeat("A", base64.StdEncoding.EncodedLen(bpsImageMaxBytes)+4097))
	require.ErrorContains(t, err, "32 MiB")
	for _, name := range []string{"../config.yaml", "/etc/passwd", ".upload-1", strings.Repeat("a", 64) + ".svg"} {
		_, _, err = store.Open(name)
		require.Error(t, err)
	}
}
func TestBPSImagesRewriteHistoryAndToolOutputsOnly(t *testing.T) {
	_, raw := bpsTestPNG(t)
	s := bpsTestGateway(t)
	body := []byte(fmt.Sprintf(
		`{"model":"gpt-5.4","input":[{"role":"user","content":[{"type":"input_image","image_url":%q}]},{"type":"function_call_output","call_id":"a","output":[{"type":"image_url","image_url":{"url":%q,"detail":"high"}}]},{"type":"custom_tool_call_output","call_id":"b","output":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":%q}}]},{"role":"user","content":[{"type":"input_text","text":"Now answer in text"}]}],"tools":[{"type":"function","parameters":{"type":"input_image","image_url":%q}}],"metadata":{"number":9007199254740993}}`, raw, raw, strings.SplitN(raw, ",", 2)[1], raw))
	out, err := s.materializeBPSInlineImages(bpsTestContext(), body)
	require.NoError(t, err)
	u := gjson.GetBytes(out, "input.0.content.0.image_url").String()
	require.True(t, strings.HasPrefix(u, s.cfg.BPSImages.PublicBaseURL+bpsImagePublicPath))
	require.NotContains(t, u, "?")
	require.Equal(t, u, gjson.GetBytes(out, "input.1.output.0.image_url").String())
	require.Equal(t, u, gjson.GetBytes(out, "input.2.output.0.image_url").String())
	require.Equal(t, "input_image", gjson.GetBytes(out, "input.1.output.0.type").String())
	require.Equal(t, "high", gjson.GetBytes(out, "input.1.output.0.detail").String())
	require.Equal(t, raw, gjson.GetBytes(out, "tools.0.parameters.image_url").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(out, "metadata.number").Raw)
	entries, err := os.ReadDir(s.bpsImageStore.root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}
func TestBPSImagesTextOnlyAndExistingURLsPassWithoutConfig(t *testing.T) {
	for _, body := range []string{`{"input":"hi"}`, `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/a.png"}]}]}`} {
		out, err := (&OpenAIGatewayService{}).materializeBPSInlineImages(nil, []byte(body))
		require.NoError(t, err)
		require.Equal(t, body, string(out))
	}
}
func TestBPSImageBaseNeverTrustsClientHeaders(t *testing.T) {
	s := bpsTestGateway(t)
	c := bpsTestContext()
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	c.Request.Header.Set("X-Forwarded-Host", "attacker.example")
	base, err := s.bpsImagePublicBaseURL(c)
	require.NoError(t, err)
	require.Equal(t, "http://192.0.2.1:8080", base)
	for _, raw := range []string{"", "https://user:pass@example.com", "https://example.com?token=1", "https://example.com#x", "file:///tmp/images"} {
		s.cfg.BPSImages.PublicBaseURL = raw
		_, err = s.bpsImagePublicBaseURL(c)
		require.Error(t, err)
	}
	s.settingService = &SettingService{}
	_, err = s.bpsImagePublicBaseURL(c)
	require.Error(t, err)
	s.cfg.BPSImages.PublicBaseURL = "https://example.com/proxy/api/v1/"
	base, err = s.bpsImagePublicBaseURL(c)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/proxy", base)
}
func TestBPSImagesInvalidOrCancelledRequestsDoNotStore(t *testing.T) {
	s := bpsTestGateway(t)
	_, raw := bpsTestPNG(t)
	body := []byte(fmt.Sprintf(`{"input":[{"role":"user","content":[{"type":"input_image","image_url":%q},{"type":"input_image","image_url":"data:image/png;base64,broken"}]}]}`, raw))
	_, err := s.materializeBPSInlineImages(bpsTestContext(), body)
	require.Error(t, err)
	entries, err := os.ReadDir(s.bpsImageStore.root)
	require.NoError(t, err)
	require.Empty(t, entries)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := bpsTestContext()
	c.Request = c.Request.WithContext(ctx)
	_, err = s.materializeBPSInlineImages(c, body)
	require.ErrorIs(t, err, context.Canceled)
}
func TestBPSImagesPublicReadHeadAndCache(t *testing.T) {
	s := bpsTestGateway(t)
	data, raw := bpsTestPNG(t)
	name, err := s.bpsImageStore.PutDataURL(raw)
	require.NoError(t, err)
	r := gin.New()
	r.GET(bpsImagePublicPath+":name", s.ServeBPSImage)
	r.HEAD(bpsImagePublicPath+":name", s.ServeBPSImage)
	for _, method := range []string{"GET", "HEAD"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, bpsImagePublicPath+name, nil))
		require.Equal(t, 200, rec.Code)
		require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
		require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		if method == "GET" {
			require.Equal(t, data, rec.Body.Bytes())
		} else {
			require.Empty(t, rec.Body.Bytes())
		}
	}
	req := httptest.NewRequest("GET", bpsImagePublicPath+name, nil)
	req.Header.Set("If-None-Match", fmt.Sprintf("%q", name))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, 304, rec.Code)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", bpsImagePublicPath+"config.yaml", nil))
	require.Equal(t, 404, rec.Code)
}

func TestBPSImagesForwardingOnlyForEnabledAccount(t *testing.T) {
	_, raw := bpsTestPNG(t)
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			wire := `data: {"type":"response.completed","response":{"id":"resp_image","status":"completed","model":"gpt-5.4","output":[],"usage":{"input_tokens":10,"output_tokens":2}}}` + "\n\n"
			up := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}}
			s := openAIClientToolsTestService(up)
			s.bpsImageStore = NewBPSImageStore(t.TempDir())
			s.cfg.BPSImages.PublicBaseURL = "http://192.0.2.1:8080"
			account := excelAccount()
			account.Extra["openai_excel_bps"] = enabled
			body := []byte(fmt.Sprintf(`{"model":"gpt-5.4","instructions":"Help","stream":true,"input":[{"role":"user","content":[{"type":"input_image","image_url":%q}]}]}`, raw))
			_, err := s.Forward(context.Background(), bpsTestContext(), account, body)
			require.NoError(t, err)
			entries, err := os.ReadDir(s.bpsImageStore.root)
			require.NoError(t, err)
			if enabled {
				require.Equal(t, "bps.openai.com", up.lastReq.URL.Host)
				require.NotContains(t, string(up.lastBody), "data:image")
				require.Contains(t, string(up.lastBody), "http://192.0.2.1:8080/api/v1/bps/images/")
				require.Len(t, entries, 1)
			} else {
				require.Equal(t, "chatgpt.com", up.lastReq.URL.Host)
				require.Contains(t, string(up.lastBody), raw)
				require.Empty(t, entries)
			}
		})
	}
}

func TestBPSImageStoreNeverReusesPartialFile(t *testing.T) {
	store := NewBPSImageStore(t.TempDir())
	_, raw := bpsTestPNG(t)
	name, err := store.PutDataURL(raw)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(store.root, name), []byte("partial"), 0600))
	_, err = store.PutDataURL(raw)
	require.Error(t, err)
}

func TestBPSImagesMCPImageToolResult(t *testing.T) {
	s := bpsTestGateway(t)
	_, raw := bpsTestPNG(t)
	body := []byte(fmt.Sprintf(`{"input":[{"type":"function_call_output","call_id":"a","output":[{"type":"image","mimeType":"image/png","data":%q}]}]}`, strings.SplitN(raw, ",", 2)[1]))
	out, err := s.materializeBPSInlineImages(bpsTestContext(), body)
	require.NoError(t, err)
	require.Equal(t, "input_image", gjson.GetBytes(out, "input.0.output.0.type").String())
	require.Contains(t, gjson.GetBytes(out, "input.0.output.0.image_url").String(), bpsImagePublicPath)
	require.False(t, gjson.GetBytes(out, "input.0.output.0.data").Exists())
	require.False(t, gjson.GetBytes(out, "input.0.output.0.mimeType").Exists())
}
