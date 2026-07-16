package extensionupdater

import "testing"

func TestCompareVersions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		left  string
		right string
		want  int
	}{
		{"0.2.0", "0.3.0", -1},
		{"1.2", "1.2.0", 0},
		{"2.0.0", "1.99.99", 1},
	}
	for _, item := range cases {
		got, err := CompareVersions(item.left, item.right)
		if err != nil {
			t.Fatalf("CompareVersions(%q, %q): %v", item.left, item.right, err)
		}
		if got != item.want {
			t.Fatalf("CompareVersions(%q, %q)=%d, want %d", item.left, item.right, got, item.want)
		}
	}
}

func TestCompareVersionsRejectsInvalid(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "1.2.3.4.5", "1.beta", "-1.0"} {
		if _, err := CompareVersions(value, "1.0.0"); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}
