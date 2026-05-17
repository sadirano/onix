package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestVersionCmd_Run(t *testing.T) {
	tests := []struct {
		name string
		json bool
	}{
		{"plain", false},
		{"json", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			cmd := &VersionCmd{}
			e := &env{JSON: tt.json}
			err := cmd.Run(context.Background(), e)

			w.Close()
			os.Stdout = oldStdout

			if err != nil {
				t.Fatalf("Run() failed: %v", err)
			}

			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			output := buf.String()

			if tt.json {
				var res struct {
					Onix   string `json:"onix"`
					Commit string `json:"commit"`
					Go     string `json:"go"`
					OSArch string `json:"os_arch"`
				}
				if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
					t.Fatalf("failed to unmarshal JSON output: %v\nOutput: %s", err, output)
				}
				if res.Onix == "" {
					t.Error("expected non-empty onix version")
				}
			} else {
				if !strings.Contains(output, "onix:") {
					t.Errorf("expected output to contain 'onix:', got: %s", output)
				}
				if !strings.Contains(output, "go:") {
					t.Errorf("expected output to contain 'go:', got: %s", output)
				}
			}
		})
	}
}
