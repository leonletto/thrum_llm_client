package endpoint

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestImageOptions_ZeroValue(t *testing.T) {
	var o ImageOptions
	if o.N != 0 || o.Prompt != "" || o.Model != "" {
		t.Errorf("zero ImageOptions has non-zero fields: %+v", o)
	}
}

func TestImageOptions_JSONRoundTrip(t *testing.T) {
	o := ImageOptions{
		Model:   "test-model",
		Prompt:  "a cat",
		Size:    "1024x1024",
		Quality: "hd",
		N:       2,
		User:    "user-123",
	}
	raw, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var back ImageOptions
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, o) {
		t.Errorf("round-trip mismatch: %+v vs %+v", back, o)
	}
}

func TestImageResult_HasImages(t *testing.T) {
	r := ImageResult{
		Created: 1234567890,
		Images:  []GeneratedImage{{URL: "https://example/x.png"}},
	}
	if len(r.Images) != 1 || r.Images[0].URL == "" {
		t.Fatalf("expected one image with URL, got %+v", r)
	}
}
