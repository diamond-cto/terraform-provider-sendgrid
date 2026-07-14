package provider_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/diamond-cto/terraform-provider-sendgrid/internal/testacc"
)

// TestAccSubuserResource_basic exercises create, disable toggle, and import.
//
// Required environment variables (in addition to SENDGRID_API_KEY / TF_ACC):
//   - TEST_SUBUSER_NEW_USERNAME: a username that does NOT yet exist
//   - TEST_SUBUSER_NEW_EMAIL:    email for the new subuser
//   - TEST_SUBUSER_PASSWORD:     password for the new subuser
//   - TEST_SUBUSER_IP:           an IP address already assigned to the parent account
func TestAccSubuserResource_basic(t *testing.T) {
	username := os.Getenv("TEST_SUBUSER_NEW_USERNAME")
	email := os.Getenv("TEST_SUBUSER_NEW_EMAIL")
	password := os.Getenv("TEST_SUBUSER_PASSWORD")
	ip := os.Getenv("TEST_SUBUSER_IP")
	if username == "" || email == "" || password == "" || ip == "" {
		t.Skip("TEST_SUBUSER_NEW_USERNAME/TEST_SUBUSER_NEW_EMAIL/TEST_SUBUSER_PASSWORD/TEST_SUBUSER_IP not set; skipping subuser resource test")
	}

	config := func(disabled bool) string {
		return fmt.Sprintf(`
resource "sendgrid_subuser" "test" {
  username = %q
  email    = %q
  password = %q
  ips      = [%q]
  disabled = %t
}
`, username, email, password, ip, disabled)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testacc.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: testacc.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create (enabled)
			{
				Config: config(false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("sendgrid_subuser.test", "id"),
					resource.TestCheckResourceAttr("sendgrid_subuser.test", "username", username),
					resource.TestCheckResourceAttr("sendgrid_subuser.test", "email", email),
					resource.TestCheckResourceAttr("sendgrid_subuser.test", "disabled", "false"),
				),
			},
			// Toggle disabled
			{
				Config: config(true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("sendgrid_subuser.test", "disabled", "true"),
				),
			},
			// Import (password/ips are not returned by the API, so they are not verified)
			{
				ResourceName:            "sendgrid_subuser.test",
				ImportState:             true,
				ImportStateId:           username,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password", "ips"},
			},
		},
	})
}
