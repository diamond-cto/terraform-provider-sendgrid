package testacc

import (
	"os"
	"testing"

	"github.com/diamond-cto/terraform-provider-sendgrid/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// TestAccProtoV6ProviderFactories is referenced from acceptance tests to
// spin up the in-memory provider server.
var TestAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"sendgrid": providerserver.NewProtocol6WithError(provider.New()),
}

// PreCheck only verifies the minimum requirements to run any acceptance test.
func PreCheck(t *testing.T) {
	if os.Getenv("SENDGRID_API_KEY") == "" {
		t.Skip("SENDGRID_API_KEY not set; skipping acceptance test")
	}
}

// TestAccPreCheck is a stricter variant used by most tests in this repo.
// It ensures tests run only when TF_ACC=1 and the API key is present.
func TestAccPreCheck(t *testing.T) {
	if v := os.Getenv("TF_ACC"); v == "" {
		t.Skip("TF_ACC=1 が必要です（受け入れテストをスキップ）")
	}
	if v := os.Getenv("SENDGRID_API_KEY"); v == "" {
		t.Fatal("SENDGRID_API_KEY が未設定です")
	}
}

// Optionally require additional variables for specific tests.
// Call this from tests that need a known subuser username, etc.
func RequireEnv(t *testing.T, vars ...string) {
	for _, k := range vars {
		if os.Getenv(k) == "" {
			t.Fatalf("%s must be set for acceptance tests", k)
		}
	}
}

// ConfigFromEnv is a small helper to build provider + data blocks inside tests.
// It intentionally avoids setting api_key and relies on SENDGRID_API_KEY.
func ConfigFromEnv(username string, onBehalfOpt string) string {
	base := os.Getenv("SENDGRID_BASE_URL") // 空なら provider 側で US デフォルト
	h := ""
	if onBehalfOpt != "" {
		h = `  on_behalf_of = "` + onBehalfOpt + `"`
	}
	return `
provider "sendgrid" {
  # api_key は未指定時、環境変数 SENDGRID_API_KEY を使用
  ` + func() string {
		if base != "" {
			return `base_url = "` + base + `"`
		}
		return ""
	}() + `
}

data "sendgrid_teammate" "t" {
  username = "` + username + `"` + `
` + h + `
}
`
}
