package main

import (
	"os"
	"os/user"
	"strings"
)

// expandUser substitutes ${user} in a personal-scope ref with the OS username.
func expandUser(ref string) string {
	if !strings.Contains(ref, "${user}") {
		return ref
	}
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	return strings.ReplaceAll(ref, "${user}", name)
}
