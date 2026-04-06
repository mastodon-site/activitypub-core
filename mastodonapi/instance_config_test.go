package mastodonapi

import (
	"encoding/json"
	"testing"

	"github.com/mastodon-site/activitypub-core/aphttp"
	"github.com/mastodon-site/activitypub-core/internal/config"
)

func TestInstancePayload_usesMediaConfiguration(t *testing.T) {
	cfg := &config.Config{
		PublicBaseURL:                "https://cfg.test",
		LocalUsername:                "alice",
		MediaMaxUploadBytes:          5 << 20,
		MediaMaxAttachmentsPerStatus: 2,
		MediaAllowedMIMETypes:        []string{"image/png"},
		MediaDescriptionLimit:        999,
	}
	h, err := aphttp.New(cfg, aphttp.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{H: h, Pool: nil}

	v1 := s.instanceV1Payload()
	raw, _ := json.Marshal(v1)
	var wrap map[string]any
	if err := json.Unmarshal(raw, &wrap); err != nil {
		t.Fatal(err)
	}
	conf, _ := wrap["configuration"].(map[string]any)
	st, _ := conf["statuses"].(map[string]any)
	if st["max_media_attachments"].(float64) != 2 {
		t.Fatalf("v1 max_media_attachments: %#v", st["max_media_attachments"])
	}
	ma, _ := conf["media_attachments"].(map[string]any)
	if ma["image_size_limit"].(float64) != float64(5<<20) {
		t.Fatalf("v1 image_size_limit: %#v", ma["image_size_limit"])
	}
	mimes, _ := ma["supported_mime_types"].([]any)
	if len(mimes) != 1 || mimes[0].(string) != "image/png" {
		t.Fatalf("v1 supported_mime_types: %#v", mimes)
	}

	v2 := s.instanceV2Payload()
	raw2, _ := json.Marshal(v2)
	var wrap2 map[string]any
	if err := json.Unmarshal(raw2, &wrap2); err != nil {
		t.Fatal(err)
	}
	conf2, _ := wrap2["configuration"].(map[string]any)
	ma2, _ := conf2["media_attachments"].(map[string]any)
	if ma2["description_limit"].(float64) != 999 {
		t.Fatalf("v2 description_limit: %#v", ma2["description_limit"])
	}
}
