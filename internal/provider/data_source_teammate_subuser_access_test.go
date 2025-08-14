package provider_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	prov "github.com/diamond-cto/terraform-provider-sendgrid/internal/provider"
)

// buildConfig returns an HCL config for the teammate_subuser_access data source.
func buildConfig(teammateName, usernameFilter string, limit, afterID string) string {
	cfg := "provider \"sendgrid\" {}\n\n"
	cfg += "data \"sendgrid_teammate_subuser_access\" \"t\" {\n"
	cfg += "  teammate_name = \"" + teammateName + "\"\n"
	if usernameFilter != "" {
		cfg += "  username = \"" + usernameFilter + "\"\n"
	}
	if limit != "" {
		cfg += "  limit = " + limit + "\n"
	}
	if afterID != "" {
		cfg += "  after_subuser_id = " + afterID + "\n"
	}
	cfg += "}\n"
	return cfg
}

// logAttr logs a single attribute's value for debugging (-v to show).
func logAttr(name, key string, t *testing.T) resource.TestCheckFunc {
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

// checkListLenGE asserts that a TypeList attribute has at least N elements.
func checkListLenGE(name, key string, minLen int, t *testing.T) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("resource not found: %s", name)
		}
		cntStr := rs.Primary.Attributes[key+".#"]
		if cntStr == "" {
			return fmt.Errorf("list count attribute missing: %s.%s.#", name, key)
		}
		var n int
		_, err := fmt.Sscanf(cntStr, "%d", &n)
		if err != nil {
			return fmt.Errorf("invalid list count for %s.%s.#: %q", name, key, cntStr)
		}
		if n < minLen {
			return fmt.Errorf("expected %s.%s length >= %d, got %d", name, key, minLen, n)
		}
		t.Logf("[DEBUG] %s.%s length=%d", name, key, n)
		return nil
	}
}

func TestAccDataTeammateSubuserAccess_basic(t *testing.T) {
	t.Parallel()

	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set; skipping acceptance test")
	}
	if os.Getenv("SENDGRID_API_KEY") == "" {
		t.Skip("SENDGRID_API_KEY not set; skipping acceptance test")
	}

	teammate := os.Getenv("TEST_TEAMMATE_NAME")
	if teammate == "" {
		t.Skip("TEST_TEAMMATE_NAME not set; skipping TestAccDataTeammateSubuserAccess_basic")
	}

	cfg := buildConfig(teammate, "", "1", "") // limit=1 で next_* の挙動確認がしやすい

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"sendgrid": providerserver.NewProtocol6WithError(prov.New()),
		},
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				// 代表的属性の存在
				resource.TestCheckResourceAttrSet("data.sendgrid_teammate_subuser_access.t", "has_restricted_subuser_access"),

				// subuser_access の構造健全性（0件も正当なため、>=0）
				checkListLenGE("data.sendgrid_teammate_subuser_access.t", "subuser_access", 0, t),

				logAttr("data.sendgrid_teammate_subuser_access.t", "subuser_access.0.username", t),
				logAttr("data.sendgrid_teammate_subuser_access.t", "subuser_access.0.scopes.0", t),

				// next_* は空または数値
				resource.TestMatchResourceAttr("data.sendgrid_teammate_subuser_access.t", "next_limit", regexp.MustCompile(`^$|^\d+$`)),
				resource.TestMatchResourceAttr("data.sendgrid_teammate_subuser_access.t", "next_after_subuser_id", regexp.MustCompile(`^$|^\d+$`)),

				// デバッグログ
				logAttr("data.sendgrid_teammate_subuser_access.t", "has_restricted_subuser_access", t),
				logAttr("data.sendgrid_teammate_subuser_access.t", "next_limit", t),
				logAttr("data.sendgrid_teammate_subuser_access.t", "next_after_subuser_id", t),
			),
		}},
	})
}

func TestAccDataTeammateSubuserAccess_noUsername(t *testing.T) {
	t.Parallel()

	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set; skipping acceptance test")
	}
	if os.Getenv("SENDGRID_API_KEY") == "" {
		t.Skip("SENDGRID_API_KEY not set; skipping acceptance test")
	}

	teammate := os.Getenv("TEST_TEAMMATE_NAME")
	if teammate == "" {
		t.Skip("TEST_TEAMMATE_NAME not set; skipping TestAccDataTeammateSubuserAccess_noUsername")
	}

	// username を指定しない（空）
	cfg := buildConfig(teammate, "", "", "")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"sendgrid": providerserver.NewProtocol6WithError(prov.New()),
		},
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				// 代表的属性の存在
				resource.TestCheckResourceAttrSet("data.sendgrid_teammate_subuser_access.t", "has_restricted_subuser_access"),
				// リストの構造（0件でも正当なので >=0）
				checkListLenGE("data.sendgrid_teammate_subuser_access.t", "subuser_access", 0, t),
				// next_* は空または数値
				resource.TestMatchResourceAttr("data.sendgrid_teammate_subuser_access.t", "next_limit", regexp.MustCompile(`^$|^\d+$`)),
				resource.TestMatchResourceAttr("data.sendgrid_teammate_subuser_access.t", "next_after_subuser_id", regexp.MustCompile(`^$|^\d+$`)),
				// デバッグログ
				logAttr("data.sendgrid_teammate_subuser_access.t", "has_restricted_subuser_access", t),
				logAttr("data.sendgrid_teammate_subuser_access.t", "next_limit", t),
				logAttr("data.sendgrid_teammate_subuser_access.t", "next_after_subuser_id", t),
			),
		}},
	})
}
