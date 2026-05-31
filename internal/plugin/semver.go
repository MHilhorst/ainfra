package plugin

import (
	"fmt"
	"strconv"
	"strings"
)

// Bump increments a MAJOR.MINOR.PATCH version by the given level
// ("major", "minor", or "patch").
func Bump(version, level string) (string, error) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("plugin: version %q is not MAJOR.MINOR.PATCH", version)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return "", fmt.Errorf("plugin: version %q has non-numeric part %q", version, p)
		}
		nums[i] = n
	}
	switch level {
	case "major":
		nums[0], nums[1], nums[2] = nums[0]+1, 0, 0
	case "minor":
		nums[1], nums[2] = nums[1]+1, 0
	case "patch":
		nums[2]++
	default:
		return "", fmt.Errorf("plugin: unknown bump level %q", level)
	}
	return fmt.Sprintf("%d.%d.%d", nums[0], nums[1], nums[2]), nil
}
