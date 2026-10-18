package version

import "testing"

func TestStable(t *testing.T) {
	c, err := NewClassifier(DefaultMarkers)
	if err != nil {
		t.Fatal(err)
	}
	for v, want := range map[string]bool{
		"1.2.0": true, "v1.2.0": true, "1.2": true, "1.2-123123": true, "1.2-fix3": true,
		"1.2.0-20251010.abc": true, "1.0.0+build.5": true, "1.0.0-develop": true, "1.0.0-preliminary": true,
		"1.2.0-rc.1": false, "1.2-beta2": false, "2.0-SNAPSHOT": false, "1.0.0-RC1": false,
		"1.0.0-alpha": false, "1.0.0-dev.20251010": false, "1.0.0-rc1-hotfix": false,
		"1.0.0-preview.3+meta": false, "1.0.0-nightly-7": false, "1.0.0-pre": false,
	} {
		if got := c.Stable(v); got != want {
			t.Errorf("Stable(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestCustomMarkers(t *testing.T) {
	c, err := NewClassifier([]string{"wip"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Stable("1.0.0-wip.2") || !c.Stable("1.0.0-rc.1") {
		t.Fatal("custom markers must replace the defaults")
	}
}

func TestNewClassifierRejectsBadMarkers(t *testing.T) {
	for _, m := range [][]string{{""}, {"r c"}, {"a.b"}} {
		if _, err := NewClassifier(m); err == nil {
			t.Errorf("NewClassifier(%q): want error", m)
		}
	}
}
