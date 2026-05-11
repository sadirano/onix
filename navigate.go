package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/sadirano/onix/internal/config"
)

// isUNCPath reports whether path is a UNC network path (\\server\share\...).
func isUNCPath(path string) bool {
	return strings.HasPrefix(path, `\\`)
}

// readLine prints prompt and reads one line from stdin.
// Returns ("", false) when the user cancels with Ctrl+C or a stream error occurs.
func readLine(prompt string) (string, bool) {
	fmt.Print(prompt)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		ch <- result{line, err}
	}()

	select {
	case <-sig:
		fmt.Println()
		os.Exit(1)
		return "", false
	case r := <-ch:
		if r.err != nil {
			return "", false
		}
		return strings.TrimSpace(r.line), true
	}
}

// promptContextConfig interactively asks the user to configure a context for
// segName. Returns the filled ContextConfig and true, or (zero, false) on
// Ctrl+C. The config is NOT written here — the caller is responsible for that.
func promptContextConfig(segName string) (config.ContextConfig, bool) {
	fmt.Printf("No context configured for segment %q\n", segName)

	var source string
	for {
		s, ok := readLine("  source [env/cmd/file/alias]: ")
		if !ok {
			return config.ContextConfig{}, false
		}
		if s == "" {
			return config.ContextConfig{}, false
		}
		switch s {
		case "env", "cmd", "file", "alias":
			source = s
		default:
			fmt.Fprintf(os.Stderr, "  unknown source %q — choose env, cmd, file, or alias\n", s)
			continue
		}
		break
	}

	cc := config.ContextConfig{Source: source}
	switch source {
	case "alias":
		path, ok := readLine("  path: ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Path = path

	case "env":
		v, ok := readLine("  var: ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Var = v
		tmpl, ok := readLine("  template (optional): ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Template = tmpl

	case "cmd":
		cmd, ok := readLine("  command: ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Cmd = cmd
		tmpl, ok := readLine("  template (optional): ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Template = tmpl

	case "file":
		f, ok := readLine("  file: ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.File = f
		tmpl, ok := readLine("  template (optional): ")
		if !ok {
			return config.ContextConfig{}, false
		}
		cc.Template = tmpl
	}

	return cc, true
}

func promptDestination(aliasName string) string {
	line, ok := readLine(fmt.Sprintf("Destination for %q: ", aliasName))
	if !ok {
		return ""
	}
	return line
}
