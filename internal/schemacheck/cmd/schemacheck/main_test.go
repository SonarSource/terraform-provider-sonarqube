package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/schemacheck"
)

func TestRun(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.json")
	head := filepath.Join(dir, "head.json")
	broken := filepath.Join(dir, "broken.json")

	if status := run([]string{"snapshot", base}, &bytes.Buffer{}, &bytes.Buffer{}); status != 0 {
		t.Fatalf("snapshot gave %d", status)
	}
	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(head, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var snapshot schemacheck.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	delete(snapshot["provider"], "url")
	data, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, data, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		args       []string
		wantStatus int
		wantOutput string
	}{
		{name: "no change", args: []string{"compare", base, head}, wantStatus: 0, wantOutput: "No breaking schema change."},
		{name: "breaking change", args: []string{"compare", base, broken}, wantStatus: 1, wantOutput: "- provider: url: removed"},
		{name: "missing file", args: []string{"compare", base, filepath.Join(dir, "missing.json")}, wantStatus: 2, wantOutput: "missing.json"},
		{name: "not a snapshot", args: []string{"compare", base, writeFile(t, dir, "[]")}, wantStatus: 2, wantOutput: "bad.json"},
		{name: "usage", args: []string{"compare", base}, wantStatus: 2, wantOutput: "usage:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if status := run(tt.args, &stdout, &stderr); status != tt.wantStatus {
				t.Errorf("status %d, want %d", status, tt.wantStatus)
			}
			if output := stdout.String() + stderr.String(); !strings.Contains(output, tt.wantOutput) {
				t.Errorf("output %q does not contain %q", output, tt.wantOutput)
			}
		})
	}
}

func writeFile(t *testing.T, dir, content string) string {
	t.Helper()
	file := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}
