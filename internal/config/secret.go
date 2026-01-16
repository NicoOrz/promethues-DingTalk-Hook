package config

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

const RedactedSecret = "<secret>"

func isBlankString(s string) bool {
	return strings.TrimSpace(s) == ""
}

type Secret string

func (s Secret) MarshalJSON() ([]byte, error) {
	if isBlankString(string(s)) {
		return []byte(`""`), nil
	}
	return []byte(`"` + RedactedSecret + `"`), nil
}

func (s *Secret) UnmarshalJSON(data []byte) error {
	if s == nil {
		return nil
	}
	if len(data) == 0 || string(data) == "null" {
		*s = ""
		return nil
	}
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v == RedactedSecret {
		*s = ""
		return nil
	}
	*s = Secret(v)
	return nil
}

func (s Secret) MarshalYAML() (any, error) {
	if isBlankString(string(s)) {
		return "", nil
	}
	return RedactedSecret, nil
}

func (s *Secret) UnmarshalYAML(value *yaml.Node) error {
	if s == nil || value == nil {
		return nil
	}
	if value.Kind != yaml.ScalarNode {
		return nil
	}
	if value.Value == RedactedSecret {
		*s = ""
		return nil
	}
	*s = Secret(value.Value)
	return nil
}

type SecretURL string

func (s SecretURL) MarshalJSON() ([]byte, error) {
	if isBlankString(string(s)) {
		return []byte(`""`), nil
	}
	return []byte(`"` + RedactedSecret + `"`), nil
}

func (s *SecretURL) UnmarshalJSON(data []byte) error {
	if s == nil {
		return nil
	}
	if len(data) == 0 || string(data) == "null" {
		*s = ""
		return nil
	}
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v == RedactedSecret {
		*s = ""
		return nil
	}
	*s = SecretURL(v)
	return nil
}

func (s SecretURL) MarshalYAML() (any, error) {
	if isBlankString(string(s)) {
		return "", nil
	}
	return RedactedSecret, nil
}

func (s *SecretURL) UnmarshalYAML(value *yaml.Node) error {
	if s == nil || value == nil {
		return nil
	}
	if value.Kind != yaml.ScalarNode {
		return nil
	}
	if value.Value == RedactedSecret {
		*s = ""
		return nil
	}
	*s = SecretURL(value.Value)
	return nil
}
