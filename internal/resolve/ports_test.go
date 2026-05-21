package resolve

import "testing"

func TestAllocatePortsAreDistinctAndStable(t *testing.T) {
	requests := []PortRequest{
		{Instance: "analytics-db", Field: "tunnelPort"},
		{Instance: "billing-db", Field: "tunnelPort"},
	}
	// No prior allocations: fresh allocation from the base.
	got, err := AllocatePorts(requests, nil, 13306)
	if err != nil {
		t.Fatalf("AllocatePorts: %v", err)
	}
	if got["analytics-db"]["tunnelPort"] == got["billing-db"]["tunnelPort"] {
		t.Error("ports collided")
	}

	// Prior allocation in the lock must be reused verbatim.
	prior := map[string]map[string]int{"analytics-db": {"tunnelPort": 19999}}
	got2, _ := AllocatePorts(requests, prior, 13306)
	if got2["analytics-db"]["tunnelPort"] != 19999 {
		t.Errorf("sticky port not reused: %d", got2["analytics-db"]["tunnelPort"])
	}
}
