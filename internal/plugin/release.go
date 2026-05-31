package plugin

import "fmt"

// Release decision actions.
const (
	ActionNoop    = "noop"
	ActionRelease = "release"
)

// Decision is the outcome of evaluating a release request.
type Decision struct {
	Action     string
	OldVersion string
	NewVersion string
}

// Decide implements the release state machine. With no bump level: unchanged
// content is a no-op, changed content is a drift error. With a bump level it
// always releases, computing the next version.
func Decide(currentHash, baselineHash, baselineVersion, bumpLevel string) (Decision, error) {
	if bumpLevel == "" {
		if currentHash == baselineHash {
			return Decision{Action: ActionNoop, OldVersion: baselineVersion, NewVersion: baselineVersion}, nil
		}
		return Decision{}, fmt.Errorf(
			"plugin content changed since v%s but version not bumped; pass --patch, --minor, or --major",
			baselineVersion)
	}
	newV, err := Bump(baselineVersion, bumpLevel)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Action: ActionRelease, OldVersion: baselineVersion, NewVersion: newV}, nil
}
