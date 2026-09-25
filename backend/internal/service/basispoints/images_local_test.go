package basispoints

import (
	"strings"
	"testing"
)

func TestOnlyConfiguredSelfHostedHTTPImagesAllowed(t *testing.T) {
	base := "http://192.0.2.1:8080"
	path := "/api/v1/bps/images/" + strings.Repeat("a", 64) + ".png"
	image := object{"type": "input_image", "image_url": base + path}
	if err := validateImage(image); err == nil {
		t.Fatal("HTTP should require explicit operator base")
	}
	if err := validateImage(image, base); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"http://evil.example" + path, base + path + "?redirect=1", base + "/other/a.png", "http://192.0.2.1:8081" + path, base + "/api/v1/bps/images/../secret.png", base + path + "#x"} {
		if err := validateImage(object{"image_url": bad}, base); err == nil {
			t.Fatalf("accepted unsafe HTTP source %s", bad)
		}
	}
}
