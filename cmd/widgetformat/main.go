// Command widgetformat rewrites the generated tables in
// docs/reference/widget-format.md from the engine contract.
//
// Same argument as cmd/surface and cmd/settings: a page kept by hand beside
// the thing it describes drifts. A test fails when the page is out of date.
package main

import (
	"fmt"
	"os"

	"github.com/mydevmachine/devmachine/internal/widgets"
)

const page = "docs/reference/widget-format.md"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	body, err := os.ReadFile(page)
	if err != nil {
		return fmt.Errorf("reading %s: %w", page, err)
	}
	updated, err := widgets.ReplaceReference(string(body))
	if err != nil {
		return fmt.Errorf("updating %s: %w", page, err)
	}
	if err := os.WriteFile(page, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", page, err)
	}
	return nil
}
