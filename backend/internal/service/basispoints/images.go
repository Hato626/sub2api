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
		return fmt.Errorf("basispoints input_image requires an HTTPS image URL or a configured self-hosted image URL without credentials")
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "data:") {
		return fmt.Errorf("basispoints does not accept data:image directly; configure Sub2API image hosting, provide an HTTPS image URL, or disable Basispoints")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || strings.TrimSpace(raw) != raw || (u.Scheme != "https" && !isLocalHTTPImage(u, bases)) {
		return fmt.Errorf("basispoints input_image requires an HTTPS image URL or a configured self-hosted image URL without credentials")
	}
	if text(part["file_id"]) != "" {
		return fmt.Errorf("basispoints input_image does not support file_id; provide an image_url")
	}
	if detail, exists := part["detail"]; exists && detail != nil {
		switch text(detail) {
		case "auto", "low", "high":
		default:
			return fmt.Errorf("basispoints image detail must be auto, low or high")
		}
	}
	return nil
}

// Only the administrator-configured host and content-addressed image path may
// use HTTP. Arbitrary third-party HTTP image URLs remain rejected.
func isLocalHTTPImage(u *url.URL, bases []string) bool {
	if u == nil || u.Scheme != "http" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || len(bases) == 0 {
		return false
	}
	b, err := url.Parse(strings.TrimRight(bases[0], "/"))
	if err != nil || b.Scheme != "http" || b.Host == "" || b.User != nil || b.RawQuery != "" || b.Fragment != "" || b.RawPath != "" {
		return false
	}
	pre := strings.TrimRight(b.Path, "/") + "/api/v1/bps/images/"
	return u.Host == b.Host && strings.HasPrefix(u.Path, pre) && localImageFilename.MatchString(strings.TrimPrefix(u.Path, pre))
}
