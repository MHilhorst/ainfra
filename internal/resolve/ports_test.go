package resolve

import "testing"

func TestAllocatePortsFreshAvoidsPriorPort(t *testing.T) {
	// analytics-db holds 13306 from the lock; a fresh billing-db request with
	// base 13306 must skip it and take 13307.
	prior := map[string]map[string]int{"analytics-db": {"tunnelPort": 13306}}
	got, err := AllocatePorts([]PortRequest{
		{Instance: "analytics-db", Field: "tunnelPort"},
		{Instance: "billing-db", Field: "tunnelPort"},
	}, prior, 13306)
	if err != nil {
		t.Fatalf("AllocatePorts: %v", err)
	}
	if got["analytics-db"]["tunnelPort"] != 13306 {
		t.Errorf("analytics-db = %d, want sticky 13306", got["analytics-db"]["tunnelPort"])
	}
	if got["billing-db"]["tunnelPort"] != 13307 {
		t.Errorf("billing-db = %d, want 13307 (13306 taken by prior)", got["billing-db"]["tunnelPort"])
	}
}

func TestAllocatePortsMultipleFieldsPerInstance(t *testing.T) {
	got, err := AllocatePorts([]PortRequest{
		{Instance: "db", Field: "tunnelPort"},
		{Instance: "db", Field: "adminPort"},
	}, nil, 13306)
	if err != nil {
		t.Fatalf("AllocatePorts: %v", err)
	}
	if got["db"]["tunnelPort"] == got["db"]["adminPort"] {
		t.Errorf("two fields of one instance collided: %v", got["db"])
	}
}

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
	got2, err := AllocatePorts(requests, prior, 13306)
	if err != nil {
		t.Fatalf("AllocatePorts: %v", err)
	}
	if got2["analytics-db"]["tunnelPort"] != 19999 {
		t.Errorf("sticky port not reused: %d", got2["analytics-db"]["tunnelPort"])
	}
}
