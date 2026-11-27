package endpoint

import (
	"maps"
	"reflect"
	"strings"
	"testing"
)

// jsonTags maps each JSON name of a struct's exported fields to its tag options.
func jsonTags(typ reflect.Type) map[string]string {
	tags := make(map[string]string, typ.NumField())
	for i := range typ.NumField() {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if !field.IsExported() || tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}
		tags[name] = options
	}
	return tags
}

// A field added to a public request type but not to its wire struct would
// silently never be sent.
func TestWireRequestJSONTagsMatchPublicRequests(t *testing.T) {
	for _, tc := range []struct {
		name         string
		public, wire reflect.Type
		// wireOptions lists the intentional tag option differences.
		wireOptions map[string]string
	}{
		{
			name:   "anthropic",
			public: reflect.TypeFor[AnthropicRequest](),
			wire:   reflect.TypeFor[anthropicWireRequest](),
		},
		{
			name:        "zai",
			public:      reflect.TypeFor[ZaiRequest](),
			wire:        reflect.TypeFor[zaiWireRequest](),
			wireOptions: map[string]string{"temperature": "omitempty"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := jsonTags(tc.public)
			maps.Copy(want, tc.wireOptions)
			if got := jsonTags(tc.wire); !maps.Equal(got, want) {
				t.Errorf("%s JSON tags (name:options) = %v, want %v", tc.wire.Name(), got, want)
			}
		})
	}
}
