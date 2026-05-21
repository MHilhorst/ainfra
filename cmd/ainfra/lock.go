package main

import (
	"fmt"
	"os"

	"github.com/MHilhorst/ainfra/internal/resolve"
)

// cmdLock implements `ainfra lock`.
func cmdLock() int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ainfra:", err)
		return 1
	}
	if err := resolve.RunLock(wd); err != nil {
		fmt.Fprintln(os.Stderr, "ainfra lock:", err)
		return 1
	}
	fmt.Println("ainfra: wrote ainfra.lock and ainfra.personal.lock")
	return 0
}
