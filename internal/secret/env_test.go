package secret

import "testing"

func TestEnvResolverReadsVariable(t *testing.T) {
	t.Setenv("AINFRA_TEST_TOKEN", "tok-123")

	got, err := EnvResolver{}.Resolve("env://AINFRA_TEST_TOKEN")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "tok-123" {
		t.Errorf("Resolve = %q, want %q", got, "tok-123")
	}
}

func TestEnvResolverUnsetVariableErrors(t *testing.T) {
	err := EnvResolver{}.Check("env://AINFRA_DEFINITELY_UNSET")
	if err == nil {
		t.Fatal("Check of unset variable: want error, got nil")
	}
}
