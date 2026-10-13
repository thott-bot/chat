// Command notices writes the licenses of Go and of each module in the app to one file for the release ZIPs. It reads
// each license from the module cache, so run `go mod download` first.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fail(errors.New("usage: go run ./tools/notices <output file>"))
	}
	var out bytes.Buffer
	goroot, err := goCommand("env", "GOROOT")
	if err != nil {
		fail(err)
	}
	if err := section(&out, "Go standard library", filepath.Join(strings.TrimSpace(string(goroot)), "LICENSE")); err != nil {
		fail(err)
	}
	list, err := goCommand("list", "-m", "-json", "all")
	if err != nil {
		fail(err)
	}
	for dec := json.NewDecoder(bytes.NewReader(list)); ; {
		var m struct {
			Path, Version, Dir string
			Main               bool
		}
		if err := dec.Decode(&m); err == io.EOF {
			break
		} else if err != nil {
			fail(err)
		}
		if m.Main {
			continue
		}
		if m.Dir == "" {
			fail(fmt.Errorf("%s %s is not in the module cache: run go mod download", m.Path, m.Version))
		}
		if err := section(&out, m.Path+" "+m.Version, filepath.Join(m.Dir, "LICENSE")); err != nil {
			fail(err)
		}
	}
	if err := os.WriteFile(os.Args[1], out.Bytes(), 0o644); err != nil {
		fail(err)
	}
}

func section(out *bytes.Buffer, name, license string) error {
	text, err := os.ReadFile(license)
	if err != nil {
		return fmt.Errorf("%s has no license file to ship: %w", name, err)
	}
	fmt.Fprintf(out, "%s\n%s\n\n%s\n\n", name, strings.Repeat("=", len(name)), bytes.TrimSpace(text))
	return nil
}

func goCommand(args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "notices:", err)
	os.Exit(1)
}
