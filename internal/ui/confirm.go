package ui

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Confirm writes prompt to w, reads one line from r, and returns true only if
// the trimmed line is exactly "yes". EOF (no line) counts as a decline. This
// is the gate apply uses before reconciling.
func Confirm(r io.Reader, w io.Writer, prompt string) (bool, error) {
	fmt.Fprint(w, prompt)
	sc := bufio.NewScanner(r)
	if !sc.Scan() {
		return false, sc.Err()
	}
	return strings.TrimSpace(sc.Text()) == "yes", nil
}
