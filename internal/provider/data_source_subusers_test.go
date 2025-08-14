package provider_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/diamond-cto/terraform-provider-sendgrid/internal/testacc"
)

func TestAccDataSubusers_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testacc.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: testacc.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
                    data "sendgrid_subusers" "t" {
                      // broad list to ensure at least one entry
                      limit          = 10
                      include_region = true
                    }
                `,
				Check: resource.ComposeTestCheckFunc(
					checkListLenGE("data.sendgrid_subusers.t", "subusers", 1, t),
					// Log a few fields for debugging/visibility
					logAttr("data.sendgrid_subusers.t", "subusers.#", t),
					logAttr("data.sendgrid_subusers.t", "subusers.0.id", t),
					logAttr("data.sendgrid_subusers.t", "subusers.0.username", t),
					logAttr("data.sendgrid_subusers.t", "subusers.0.email", t),
					logAttr("data.sendgrid_subusers.t", "subusers.0.disabled", t),
					// Basic sanity checks on the first element
					resource.TestCheckResourceAttrSet("data.sendgrid_subusers.t", "subusers.0.id"),
					resource.TestCheckResourceAttrSet("data.sendgrid_subusers.t", "subusers.0.username"),
					resource.TestCheckResourceAttrSet("data.sendgrid_subusers.t", "subusers.0.email"),
				),
			},
		},
	})
}

func TestAccDataSubusers_filterByUsername(t *testing.T) {
	username := os.Getenv("TEST_SUBUSER_USERNAME")
	if username == "" {
		t.Skip("TEST_SUBUSER_USERNAME not set; skipping username filter test")
	}

	cfg := fmt.Sprintf(`
        data "sendgrid_subusers" "t" {
          username = %q
          limit    = 1
        }
    `, username)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testacc.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: testacc.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					// exactly 1 match expected when username fully specified
					resource.TestCheckResourceAttr("data.sendgrid_subusers.t", "subusers.#", "1"),
					resource.TestCheckResourceAttr("data.sendgrid_subusers.t", "subusers.0.username", username),
					resource.TestCheckResourceAttrSet("data.sendgrid_subusers.t", "subusers.0.id"),
					resource.TestCheckResourceAttrSet("data.sendgrid_subusers.t", "subusers.0.email"),
				),
			},
		},
	})
}
