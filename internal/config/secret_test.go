package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSecret_JSON_RedactsWhenSet(t *testing.T) {
	b, err := json.Marshal(Secret("real"))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got != RedactedSecret {
		t.Fatalf("got=%q", got)
	}
}

func TestSecret_JSON_EmptyWhenBlank(t *testing.T) {
	b, err := json.Marshal(Secret(""))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got != "" {
		t.Fatalf("got=%q", got)
	}
}

func TestSecret_JSON_UnmarshalRedactedToEmpty(t *testing.T) {
	var s Secret
	if err := json.Unmarshal([]byte(`"<secret>"`), &s); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if s != "" {
		t.Fatalf("s=%q", string(s))
	}
}

func TestSecret_YAML_RedactsWhenSet(t *testing.T) {
	b, err := yaml.Marshal(struct {
		S Secret `yaml:"s"`
	}{S: Secret("real")})
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	if string(b) != "s: <secret>\n" {
		t.Fatalf("yaml=%q", string(b))
	}
}

func TestSecretURL_JSON_RedactsWhenSet(t *testing.T) {
	b, err := json.Marshal(SecretURL("http://example.invalid"))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got != RedactedSecret {
		t.Fatalf("got=%q", got)
	}
}
