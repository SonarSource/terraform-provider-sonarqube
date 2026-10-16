package cloud_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestMain runs the sweepers of package acctest when the run has -sweep.
// Without -sweep, it runs the tests.
func TestMain(m *testing.M) {
	resource.TestMain(m)
}
