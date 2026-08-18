package version

import "testing"

func TestGetVersionUsesLinkerSettableVersion(t *testing.T) {
	original := version
	version = " v-test "
	t.Cleanup(func() { version = original })

	if got := GetVersion(); got != "v-test" {
		t.Fatalf("GetVersion() = %q, want v-test", got)
	}
}
