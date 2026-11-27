package launch

import (
	"reflect"
	"testing"
)

func TestBuildForHermesUsesNativeSessionRouting(t *testing.T) {
	for _, tc := range []struct {
		mode Mode
		dir  string
		args []string
	}{
		{New, "/current", []string{"/bin/hermes"}},
		{Resume, "/recorded", []string{"/bin/hermes", "--resume", "20260101_120000_abcd12"}},
	} {
		got := BuildFor("hermes", "/bin/hermes", tc.mode, "20260101_120000_abcd12", "/recorded", "/current", "saved-provider-model")
		if got.Dir != tc.dir || !reflect.DeepEqual(got.Args, tc.args) {
			t.Errorf("mode %v: got %+v, want %v in %q", tc.mode, got, tc.args, tc.dir)
		}
	}
}
