package main

import (
	"fmt"
	"os"

	"github.com/MHilhorst/aistack/internal/resolve"
)

// cmdLock implements `aistack lock`.
func cmdLock() int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "aistack:", err)
		return 1
	}
	if err := resolve.RunLock(wd); err != nil {
		fmt.Fprintln(os.Stderr, "aistack lock:", err)
		return 1
	}
	fmt.Println("aistack: wrote ai-stack.lock")
	return 0
}
