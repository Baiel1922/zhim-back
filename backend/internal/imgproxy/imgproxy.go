// Package imgproxy builds signed imgproxy URLs for product photos stored in S3.
//
// URLs carry no format extension: imgproxy picks AVIF or WebP from the browser's
// Accept header (IMGPROXY_AUTO_AVIF / IMGPROXY_AUTO_WEBP).
package imgproxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Preset is a named set of widths for one usage (grid card, gallery, ...).
// Photos are cropped to a 4:5 portrait aspect.
type Preset struct {
	Widths       []int
	DefaultWidth int
}

// Presets used by the storefront.
var (
	Thumb = Preset{Widths: []int{120, 240}, DefaultWidth: 120}
	Card  = Preset{Widths: []int{360, 540, 720, 1080}, DefaultWidth: 540}
	Full  = Preset{Widths: []int{828, 1200, 1600, 2048}, DefaultWidth: 1200}
)

// Source is a rendered preset: default URL plus a srcset with width descriptors.
type Source struct {
	Src    string
	Srcset string
}

// Signer signs imgproxy URLs.
type Signer struct {
	baseURL string
	bucket  string
	key     []byte
	salt    []byte
}

// NewSigner creates a Signer. key and salt are hex-encoded, as in IMGPROXY_KEY/IMGPROXY_SALT.
func NewSigner(baseURL, bucket, keyHex, saltHex string) (*Signer, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) == 0 {
		return nil, errors.New("imgproxy: key must be non-empty hex")
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil || len(salt) == 0 {
		return nil, errors.New("imgproxy: salt must be non-empty hex")
	}
	if baseURL == "" || bucket == "" {
		return nil, errors.New("imgproxy: base url and bucket are required")
	}
	return &Signer{baseURL: strings.TrimRight(baseURL, "/"), bucket: bucket, key: key, salt: salt}, nil
}

// URL returns a signed URL that crops the S3 object to width×(width*5/4).
func (s *Signer) URL(s3Key string, width int) string {
	height := width * 5 / 4
	source := base64.RawURLEncoding.EncodeToString([]byte("s3://" + s.bucket + "/" + s3Key))
	path := "/rs:fill:" + strconv.Itoa(width) + ":" + strconv.Itoa(height) + ":0/g:sm/" + source
	return s.baseURL + "/" + s.sign(path) + path
}

// Source renders a preset for the S3 object.
func (s *Signer) Source(s3Key string, p Preset) Source {
	parts := make([]string, len(p.Widths))
	for i, w := range p.Widths {
		parts[i] = fmt.Sprintf("%s %dw", s.URL(s3Key, w), w)
	}
	return Source{Src: s.URL(s3Key, p.DefaultWidth), Srcset: strings.Join(parts, ", ")}
}

func (s *Signer) sign(path string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(s.salt)
	mac.Write([]byte(path))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
