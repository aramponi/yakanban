package cli

import "testing"

func TestCheck(t *testing.T) {
	for _, ok := range [][]string{
		{"board"},
		{"move", "7", "In Progress", "--claim", "lucid-onyx-11"},
		{"create", "A title", "--priority", "high", "--tags", "bug"},
		{"delete", "8", "--yes"},
	} {
		if err := Check(ok); err != nil {
			t.Errorf("Check(%q) = %v", ok, err)
		}
	}
	for _, bad := range [][]string{
		{"frobnicate"},
		{"move", "7", "Done", "--no-such-flag"},
	} {
		if err := Check(bad); err == nil {
			t.Errorf("Check(%q) accepted it", bad)
		}
	}
}
