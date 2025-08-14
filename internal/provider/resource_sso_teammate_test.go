package provider_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	prov "github.com/diamond-cto/terraform-provider-sendgrid/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	resource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/sendgrid/sendgrid-go"
)

// buildResourceConfig returns an HCL config for sendgrid_sso_teammate.
func buildResourceConfig(email, firstName, lastName string, subuserID string) string {
	cfg := "provider \"sendgrid\" {}\n\n"
	cfg += "resource \"sendgrid_sso_teammate\" \"test\" {\n"
	cfg += "  email      = \"" + email + "\"\n"
	if firstName != "" {
		cfg += "  first_name = \"" + firstName + "\"\n"
	}
	if lastName != "" {
		cfg += "  last_name  = \"" + lastName + "\"\n"
	}
	cfg += "  has_restricted_subuser_access = true\n"
	cfg += "  subuser_access {\n"
	cfg += "    id              = " + subuserID + "\n"
	cfg += "    permission_type = \"restricted\"\n"
	cfg += "    scopes = [\n"
	cfg += "      \"mail_settings.read\",\n"
	cfg += "      \"messages.read\",\n"
	cfg += "      \"partner_settings.read\",\n"
	cfg += "      \"stats.read\",\n"
	cfg += "      \"tracking_settings.read\",\n"
	cfg += "      \"user.account.read\",\n"
	cfg += "      \"user.credits.read\",\n"
	cfg += "      \"user.email.read\",\n"
	cfg += "      \"user.profile.read\",\n"
	cfg += "      \"user.settings.enforced_tls.read\",\n"
	cfg += "      \"user.timezone.read\",\n"
	cfg += "      \"user.username.read\",\n"
	cfg += "    ]\n"
	cfg += "  }\n"
	cfg += "}\n"
	return cfg
}

// checkSubuserHasPermissionAndScope finds the subuser_access entry with the given id
// and asserts its permission_type and that at least one scope exists (optionally that a requiredScope exists).
func checkSubuserHasPermissionAndScope(resourceName, expectedID, expectedPerm, requiredScope string, t *testing.T) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource not found: %s", resourceName)
		}
		// Find index where subuser_access.<i>.id == expectedID
		attrs := rs.Primary.Attributes
		foundIdx := ""
		for k, v := range attrs {
			if !strings.HasPrefix(k, "subuser_access.") || !strings.HasSuffix(k, ".id") {
				continue
			}
			if v == expectedID {
				// key form: subuser_access.<i>.id
				parts := strings.Split(k, ".")
				if len(parts) >= 3 {
					foundIdx = parts[1]
					break
				}
			}
		}
		if foundIdx == "" {
			return fmt.Errorf("subuser id %s not found in state", expectedID)
		}
		// Check permission_type
		permKey := fmt.Sprintf("subuser_access.%s.permission_type", foundIdx)
		if p, ok := attrs[permKey]; !ok || p != expectedPerm {
			return fmt.Errorf("expected %s to be %q, got %q", permKey, expectedPerm, attrs[permKey])
		}
		// Ensure at least one scope
		scopesCountKey := fmt.Sprintf("subuser_access.%s.scopes.#", foundIdx)
		cnt, ok := attrs[scopesCountKey]
		if !ok || cnt == "" {
			return fmt.Errorf("missing scopes count at %s", scopesCountKey)
		}
		var n int
		if _, err := fmt.Sscanf(cnt, "%d", &n); err != nil {
			return fmt.Errorf("invalid scopes count %q: %v", cnt, err)
		}
		if n == 0 {
			return fmt.Errorf("expected at least one scope for subuser %s", expectedID)
		}
		if requiredScope != "" {
			// scan scopes.* for a match
			requiredFound := false
			prefix := fmt.Sprintf("subuser_access.%s.scopes.", foundIdx)
			for k, v := range attrs {
				if strings.HasPrefix(k, prefix) && !strings.HasSuffix(k, ".#") {
					if v == requiredScope {
						requiredFound = true
						break
					}
				}
			}
			if !requiredFound {
				return fmt.Errorf("required scope %q not found for subuser %s", requiredScope, expectedID)
			}
		}
		t.Logf("[DEBUG] matched subuser id=%s index=%s perm=%s", expectedID, foundIdx, expectedPerm)
		return nil
	}
}

func testAccCheckSSOTeammateDestroy(t *testing.T) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		email := os.Getenv("TEST_SSO_EMAIL")
		if email == "" {
			t.Log("TEST_SSO_EMAIL not set; skipping remote delete verification")
			return nil
		}
		apiKey := os.Getenv("SENDGRID_API_KEY")
		if apiKey == "" {
			return fmt.Errorf("SENDGRID_API_KEY not set")
		}
		baseURL := os.Getenv("SENDGRID_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.sendgrid.com"
		}

		req := sendgrid.GetRequest(apiKey, "/v3/teammates/"+email, baseURL)
		req.Method = "GET"
		resp, err := sendgrid.API(req)
		if err != nil {
			return fmt.Errorf("SendGrid API error: %v", err)
		}
		if resp.StatusCode == 404 {
			return nil
		}
		if resp.StatusCode >= 300 {
			return fmt.Errorf("unexpected status checking deletion: %d body=%s", resp.StatusCode, resp.Body)
		}
		return fmt.Errorf("teammate %s still exists (status=%d)", email, resp.StatusCode)
	}
}

func TestAccResourceSSOTeammate_CRUD_Import(t *testing.T) {
	t.Parallel()

	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set; skipping acceptance test")
	}
	if os.Getenv("SENDGRID_API_KEY") == "" {
		t.Skip("SENDGRID_API_KEY not set; skipping acceptance test")
	}

	rSuffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	email := fmt.Sprintf("terraform-acctest-%s@example.com", rSuffix)
	subID := os.Getenv("TEST_SUBUSER_ID")
	if email == "" || subID == "" {
		t.Skip("TEST_SSO_EMAIL or TEST_SUBUSER_ID not set; skipping TestAccResourceSSOTeammate_CRUD_Import")
	}

	cfgCreate := buildResourceConfig(email, "Terraform", "AccTest", subID)
	cfgUpdate := buildResourceConfig(email, "Terraform-Updated", "AccTest", subID)

	resourceName := "sendgrid_sso_teammate.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"sendgrid": providerserver.NewProtocol6WithError(prov.New()),
		},
		CheckDestroy: testAccCheckSSOTeammateDestroy(t),
		Steps: []resource.TestStep{
			// CREATE
			{
				Config: cfgCreate,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "email", email),
					resource.TestMatchResourceAttr(resourceName, "status", regexp.MustCompile(`^(|active|pending)$`)),
					checkListLenGE(resourceName, "subuser_access", 1, t),
					logAttr(resourceName, "subuser_access.0.permission_type", t),
					checkSubuserHasPermissionAndScope(resourceName, subID, "restricted", "messages.read", t),
				),
			},
			// UPDATE (change first_name)
			{
				Config: cfgUpdate,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "first_name", "Terraform-Updated"),
					checkListLenGE(resourceName, "subuser_access", 1, t),
				),
			},
			// IMPORT & VERIFY
			{
				ResourceName:                         resourceName,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateId:                        email, // import by username/email
				ImportStateVerifyIdentifierAttribute: "email",
			},
			// PLAN-ONLY: ensure no diff after import
			{
				Config:             cfgUpdate,
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
			// DESTROY: must provide a valid config; the framework destroys the resources defined by this config
			{
				Destroy: true,
				Config:  cfgUpdate,
			},
		},
	})
}
