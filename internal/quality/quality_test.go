package quality

import (
	"strings"
	"testing"
)

func TestNormalizeExtensions(t *testing.T) {
	got, err := NormalizeExtensions([]string{"ISO", " *.img ", ".wmv", "iso", "", "."})
	if err != nil || strings.Join(got, ",") != ".iso,.img,.wmv" {
		t.Fatalf("%v %v", got, err)
	}
	for _, bad := range []string{"a b", "../x", ".exe;rm", "x/y"} {
		if _, err := NormalizeExtensions([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if FileRisk("Bonus.ISO", []string{".iso"}) != "rejected" || FileRisk("Movie.mkv", []string{".iso"}) != "" || FileRisk("x.exe", []string{".exe"}) != "executable" {
		t.Fatal("FileRisk with a user list")
	}
}
