package segments

import (
	"strings"
	"testing"
)

// TestExpandTemplate locks the variable-substitution contract.
func TestExpandTemplate(t *testing.T) {
	lookup := func(env map[string]string) LookupFunc {
		return func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		}
	}

	tests := []struct {
		name    string
		tmpl    string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{
			name: "no_vars_passthrough",
			tmpl: "/static/path",
			want: "/static/path",
		},
		{
			name: "single_var",
			tmpl: "/${client}",
			env:  map[string]string{"client": "bob"},
			want: "/bob",
		},
		{
			name: "multi_var",
			tmpl: "/${a}/${b}/x",
			env:  map[string]string{"a": "one", "b": "two"},
			want: "/one/two/x",
		},
		{
			name: "empty_value_substitutes_empty",
			tmpl: "/${empty}/suffix",
			env:  map[string]string{"empty": ""},
			want: "//suffix",
		},
		{
			name:    "missing_var_errors",
			tmpl:    "/${nope}",
			env:     map[string]string{},
			wantErr: true,
		},
		{
			name: "dollar_without_brace_is_literal",
			tmpl: "$5 and $foo",
			want: "$5 and $foo",
		},
		{
			name:    "unterminated_errors",
			tmpl:    "/${dangling",
			wantErr: true,
		},
		{
			name:    "empty_name_errors",
			tmpl:    "/${}",
			wantErr: true,
		},
		{
			name: "dollar_at_end_is_literal",
			tmpl: "trailing$",
			want: "trailing$",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExpandTemplate(tc.tmpl, "test", lookup(tc.env))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestExpandTemplate_ErrorMentionsVar guarantees the error string carries
// the variable name and the caller-supplied location.
func TestExpandTemplate_ErrorMentionsVar(t *testing.T) {
	_, err := ExpandTemplate("/${missing}/x", "source-template", func(string) (string, bool) {
		return "", false
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should mention variable name 'missing': %v", err)
	}
	if !strings.Contains(err.Error(), "source-template") {
		t.Errorf("error should mention the where label: %v", err)
	}
}

// TestGuardFragment covers each rejection rule of the traversal guard.
func TestGuardFragment(t *testing.T) {
	tests := []struct {
		name     string
		fragment string
		wantErr  bool
	}{
		{"leading_slash_ok", "/docs", false},
		{"nested_path_ok", "/foo/bar", false},
		{"filename_suffix_ok", "_432.md", false},
		{"empty_ok", "", false},

		{"double_leading_slash", "//etc", true},
		{"leading_backslash", "\\etc", true},
		{"slash_then_backslash", "/\\foo", true},
		{"leading_tilde", "~/secret", true},
		{"drive_letter_uppercase", "C:/foo", true},
		{"drive_letter_lowercase", "c:foo", true},
		// Drive letter after the single leading `/` is also caught — the
		// stripped fragment still begins with the disallowed pattern.
		{"drive_letter_after_slash", "/C:/foo", true},

		{"dotdot_at_start", "../etc", true},
		{"dotdot_in_middle", "/foo/../bar", true},
		{"dotdot_at_end", "/foo/..", true},
		{"dotdot_backslash", "foo\\..\\bar", true},

		{"single_dot_ok", "/foo/./bar", false},
		{"dotdot_substring_ok", "/foo..bar", false},

		{"null_byte", "/foo\x00bar", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := GuardFragment("seg", tc.fragment)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tc.fragment)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for %q: %v", tc.fragment, err)
			}
		})
	}
}

// TestGuardFragment_ErrorMentionsSegment checks the caller doesn't have to
// wrap to give the user context.
func TestGuardFragment_ErrorMentionsSegment(t *testing.T) {
	err := GuardFragment("client", "../escape")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "client") {
		t.Errorf("error should mention segment name 'client': %v", err)
	}
}
