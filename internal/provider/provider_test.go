package provider

import "testing"

func TestChannelPlanEmpty(t *testing.T) {
	empty := ChannelPlan{Channel: "skills", Changes: []Change{{Kind: ChangeNoop, ID: "a"}}}
	if !empty.Empty() {
		t.Error("a plan of only noop changes must be Empty")
	}
	busy := ChannelPlan{Channel: "skills", Changes: []Change{{Kind: ChangeCreate, ID: "a"}}}
	if busy.Empty() {
		t.Error("a plan with a create must not be Empty")
	}
}
