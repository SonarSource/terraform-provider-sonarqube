package acctest

import (
	"os"
	"testing"
)

// TestMain makes the unit tests of this package independent of
// SONARQUBE_TEST_SOT_TOKEN in the shell. A test that needs SOT mode sets it
// with setSOTMode.
func TestMain(m *testing.M) {
	sotToken, userToken = "", ""
	os.Exit(m.Run())
}
