package provider_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/diamond-cto/terraform-provider-sendgrid/internal/provider"
	"github.com/diamond-cto/terraform-provider-sendgrid/internal/testacc"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccDataTeammate_basic(t *testing.T) {
	t.Parallel() // ← Data Source は読み取りのみなので並列でもOK。レート制限が厳しいなら外してください。

	testacc.PreCheck(t)

	username := os.Getenv("TEST_USERNAME")
	if username == "" {
		t.Skip("TEST_USERNAME not set; skipping TestAccDataTeammate_basic")
	}
	onBehalf := os.Getenv("TEST_ON_BEHALF_OF") // 空なら使わない

	cfg := testacc.ConfigFromEnv(username, onBehalf)

	// Helper: log attribute value for debugging
	logAttr := func(name, key string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			rs, ok := s.RootModule().Resources[name]
			if !ok {
				return fmt.Errorf("resource not found: %s", name)
			}
			v, ok := rs.Primary.Attributes[key]
			if !ok {
				return fmt.Errorf("attribute not found: %s.%s", name, key)
			}
			t.Logf("[DEBUG] %s.%s=%s", name, key, v)
			return nil
		}
	}

	// Helper: ensure a TypeSet has at least N elements (uses ".#" count attr)
	checkSetLenGE := func(name, key string, minLen int) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			rs, ok := s.RootModule().Resources[name]
			if !ok {
				return fmt.Errorf("resource not found: %s", name)
			}
			cntStr := rs.Primary.Attributes[key+".#"]
			if cntStr == "" {
				return fmt.Errorf("set count attribute missing: %s.%s.#", name, key)
			}
			var n int
			_, err := fmt.Sscanf(cntStr, "%d", &n)
			if err != nil {
				return fmt.Errorf("invalid set count for %s.%s.#: %q", name, key, cntStr)
			}
			if n < minLen {
				return fmt.Errorf("expected %s.%s length >= %d, got %d", name, key, minLen, n)
			}
			t.Logf("[DEBUG] %s.%s length=%d", name, key, n)
			return nil
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"sendgrid": providerserver.NewProtocol6WithError(provider.New()),
		},
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					// 代表的な属性を確認（存在チェック）
					resource.TestCheckResourceAttrSet("data.sendgrid_teammate.t", "email"),
					resource.TestCheckResourceAttrSet("data.sendgrid_teammate.t", "user_type"),
					resource.TestCheckResourceAttrSet("data.sendgrid_teammate.t", "is_admin"),
					// Pattern checks
					resource.TestMatchResourceAttr("data.sendgrid_teammate.t", "user_type", regexp.MustCompile(`^(owner|admin|teammate)$`)),
					resource.TestMatchResourceAttr("data.sendgrid_teammate.t", "email", regexp.MustCompile(`^[^@\n]+@[^@\n]+\.[^@\n]+$`)),
					resource.TestMatchResourceAttr("data.sendgrid_teammate.t", "is_admin", regexp.MustCompile(`^(true|false)$`)),

					// Ensure scopes set has at least one element
					checkSetLenGE("data.sendgrid_teammate.t", "scopes", 1),

					// Debug logs (visible with -v)
					logAttr("data.sendgrid_teammate.t", "email"),
					logAttr("data.sendgrid_teammate.t", "user_type"),
					logAttr("data.sendgrid_teammate.t", "is_admin"),
				),
			},
		},
	})
}
