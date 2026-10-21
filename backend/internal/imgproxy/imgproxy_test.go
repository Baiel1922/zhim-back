package imgproxy_test

import (
	"strings"
	"testing"

	"github.com/Baiel1922/zhim-back/backend/internal/imgproxy"
)

// Test key/salt (imgproxy docs example values); the expected signature was computed
// with an independent HMAC-SHA256 implementation verified against imgproxy v4.
const (
	testKey  = "943b421c9eb07c830af81030552c86009268de4e532ba2ee2eab8247c6da0881"
	testSalt = "520f986b998545b4785e0defbc4f3c1203f22de2374a3d53cb7a7fe9fea309c5"
)

func TestURL(t *testing.T) {
	s, err := imgproxy.NewSigner("https://img.zhim.kg/", "zhim", testKey, testSalt)
	if err != nil {
		t.Fatal(err)
	}
	got := s.URL("products/core/1.jpg", 540)

	// base64url("s3://zhim/products/core/1.jpg") without padding; 540×675 is 4:5.
	const want = "https://img.zhim.kg/L7dv7ukwWvO13vnqozgc1YhUYE4Ic9KWL7FFiyXn0cM" +
		"/rs:fill:540:675:0/g:sm/czM6Ly96aGltL3Byb2R1Y3RzL2NvcmUvMS5qcGc"
	if got != want {
		t.Fatalf("url =\n%s\nwant\n%s", got, want)
	}
}

func TestSource(t *testing.T) {
	s, err := imgproxy.NewSigner("http://localhost:8081", "zhim", testKey, testSalt)
	if err != nil {
		t.Fatal(err)
	}
	src := s.Source("a.jpg", imgproxy.Card)
	if !strings.Contains(src.Src, "/rs:fill:540:675:0/") {
		t.Errorf("default src = %s", src.Src)
	}
	entries := strings.Split(src.Srcset, ", ")
	if len(entries) != len(imgproxy.Card.Widths) || !strings.HasSuffix(entries[0], " 360w") || !strings.HasSuffix(entries[3], " 1080w") {
		t.Errorf("srcset = %s", src.Srcset)
	}
}

func TestNewSignerValidates(t *testing.T) {
	if _, err := imgproxy.NewSigner("http://x", "b", "not-hex", testSalt); err == nil {
		t.Error("want error for bad key")
	}
	if _, err := imgproxy.NewSigner("http://x", "b", testKey, ""); err == nil {
		t.Error("want error for empty salt")
	}
	if _, err := imgproxy.NewSigner("", "b", testKey, testSalt); err == nil {
		t.Error("want error for empty base url")
	}
}
