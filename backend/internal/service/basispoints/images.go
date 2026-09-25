package basispoints

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var localImageFilename = regexp.MustCompile("^[a-f0-9]{64}\\.(png|jpg|webp|gif)$")

func validateImage(part object, bases ...string) error {
	raw, ok := part["image_url"].(string)
	if !ok || raw == "" {
		return fmt.Errorf("HTTPS image_url required")
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "data:") {
		return fmt.Errorf("data image unsupported; provide HTTP(S) URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || strings.TrimSpace(raw) != raw || (u.Scheme != "https" && !isLocalHTTPImage(u, bases)) {
		return fmt.Errorf("HTTPS image_url required")
	}
	if text(part["file_id"]) != "" {
		return fmt.Errorf("file_id unsupported")
	}
	return nil
}
func isLocalHTTPImage(u *url.URL, bases []string) bool {
	if u == nil || u.Scheme != "http" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || len(bases) == 0 {
		return false
	}
	b, err := url.Parse(strings.TrimRight(bases[0], "/"))
	if err != nil || b.Scheme != "http" || b.Host == "" || b.User != nil {
		return false
	}
	pre := strings.TrimRight(b.Path, "/") + "/api/v1/bps/images/"
	return u.Host == b.Host && strings.HasPrefix(u.Path, pre) && localImageFilename.MatchString(strings.TrimPrefix(u.Path, pre))
}
