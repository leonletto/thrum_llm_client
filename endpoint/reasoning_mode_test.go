package endpoint

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReasoningMode_ZeroValueIsOff(t *testing.T) {
	var m ReasoningMode
	if m != ReasoningModeOff {
		t.Fatalf("zero value = %v; want ReasoningModeOff", m)
	}
}

func TestReasoningMode_String(t *testing.T) {
	cases := map[ReasoningMode]string{
		ReasoningModeOff:  "off",
		ReasoningModeAuto: "auto",
		ReasoningModeOn:   "on",
	}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("ReasoningMode(%d).String() = %q; want %q", m, got, want)
		}
	}
}

func TestReasoningMode_MarshalYAML(t *testing.T) {
	type holder struct {
		Mode ReasoningMode `yaml:"mode"`
	}
	cases := []struct {
		mode ReasoningMode
		want string
	}{
		// yaml.v3 quotes "off"/"on" because they collide with YAML 1.1
		// bool literals; "auto" is unambiguous and emitted bare.
		{ReasoningModeOff, "mode: \"off\"\n"},
		{ReasoningModeAuto, "mode: auto\n"},
		{ReasoningModeOn, "mode: \"on\"\n"},
	}
	for _, tc := range cases {
		out, err := yaml.Marshal(holder{Mode: tc.mode})
		if err != nil {
			t.Fatalf("marshal %v: %v", tc.mode, err)
		}
		if string(out) != tc.want {
			t.Errorf("marshal %v = %q; want %q", tc.mode, string(out), tc.want)
		}
	}
}

func TestReasoningMode_UnmarshalYAML(t *testing.T) {
	type holder struct {
		Mode ReasoningMode `yaml:"mode"`
	}
	cases := []struct {
		in   string
		want ReasoningMode
	}{
		{"mode: off\n", ReasoningModeOff},
		{"mode: auto\n", ReasoningModeAuto},
		{"mode: on\n", ReasoningModeOn},
		{"{}\n", ReasoningModeOff},
	}
	for _, tc := range cases {
		var h holder
		if err := yaml.Unmarshal([]byte(tc.in), &h); err != nil {
			t.Fatalf("unmarshal %q: %v", tc.in, err)
		}
		if h.Mode != tc.want {
			t.Errorf("unmarshal %q = %v; want %v", tc.in, h.Mode, tc.want)
		}
	}
}

func TestReasoningMode_UnmarshalYAML_Invalid(t *testing.T) {
	type holder struct {
		Mode ReasoningMode `yaml:"mode"`
	}
	var h holder
	err := yaml.Unmarshal([]byte("mode: bogus\n"), &h)
	if err == nil {
		t.Fatal("expected error for invalid value")
	}
}

func TestReasoningMode_JSONRoundTrip(t *testing.T) {
	type holder struct {
		Mode ReasoningMode `json:"mode"`
	}
	for _, m := range []ReasoningMode{ReasoningModeOff, ReasoningModeAuto, ReasoningModeOn} {
		raw, err := json.Marshal(holder{Mode: m})
		if err != nil {
			t.Fatalf("marshal %v: %v", m, err)
		}
		var back holder
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if back.Mode != m {
			t.Errorf("round-trip %v -> %s -> %v", m, raw, back.Mode)
		}
	}
}
