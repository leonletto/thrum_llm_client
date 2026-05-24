package endpoint

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ReasoningMode controls how a reasoning-capable provider's "thinking"
// (or equivalent) wire field is emitted on chat requests. The zero value
// is ReasoningModeOff: the field is sent explicitly as
// {"type":"disabled"} when the caller passes EnableThinking=false.
//
// This type is a forward-compatible enum. New values may be added (e.g.,
// for tool-mode-style provider extensions) without breaking the wire
// contract for existing modes.
type ReasoningMode int

const (
	// ReasoningModeOff (default) emits the reasoning control field
	// explicitly. For Z.ai this is {"type":"disabled"} when
	// EnableThinking=false and {"type":"enabled"} when true. Use this
	// for any model that accepts an explicit disabled control — this
	// is the historical thrum_llm_client default and the safe choice
	// for reasoning models like GLM-5.1.
	ReasoningModeOff ReasoningMode = iota

	// ReasoningModeAuto omits the reasoning control field on the wire
	// when EnableThinking=false (the model picks its own default), and
	// emits {"type":"enabled"} when true. Use this only for models
	// that genuinely reject the explicit disabled form.
	ReasoningModeAuto

	// ReasoningModeOn forces the reasoning control field to enabled
	// regardless of the caller's EnableThinking value. Use for
	// always-reasoning models that cannot truly disable thinking.
	ReasoningModeOn
)

// String returns the lowercase name of the mode.
func (m ReasoningMode) String() string {
	switch m {
	case ReasoningModeOff:
		return "off"
	case ReasoningModeAuto:
		return "auto"
	case ReasoningModeOn:
		return "on"
	default:
		return fmt.Sprintf("ReasoningMode(%d)", int(m))
	}
}

func parseReasoningMode(s string) (ReasoningMode, error) {
	switch s {
	case "", "off":
		return ReasoningModeOff, nil
	case "auto":
		return ReasoningModeAuto, nil
	case "on":
		return ReasoningModeOn, nil
	default:
		return 0, fmt.Errorf("invalid reasoning_mode %q (want off|auto|on)", s)
	}
}

// MarshalYAML emits the lowercase mode name.
func (m ReasoningMode) MarshalYAML() (any, error) {
	return m.String(), nil
}

// UnmarshalYAML accepts off|auto|on, with empty mapping to off.
func (m *ReasoningMode) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	v, err := parseReasoningMode(s)
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// MarshalJSON emits the lowercase mode name as a JSON string.
func (m ReasoningMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

// UnmarshalJSON accepts off|auto|on, with empty mapping to off.
func (m *ReasoningMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, err := parseReasoningMode(s)
	if err != nil {
		return err
	}
	*m = v
	return nil
}
