package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/diamond-cto/terraform-provider-sendgrid/internal/testacc"
)

// These tests exercise the sendgrid_subuser resource against a local httptest
// server that emulates the SendGrid Subusers API, so they do NOT require
// TF_ACC or a real SENDGRID_API_KEY and run as part of `go test ./...`.
//
// The emulated response shapes are based on the official API documentation:
//   - POST /v3/subusers returns {username, user_id, email, credit_allocation, region}
//     (note: the id field is named "user_id", and the response omits "ips")
//   - GET  /v3/subusers?username=... returns a list of {id, username, email, disabled, region}
//   - PATCH  /v3/subusers/{username} toggles the disabled flag
//   - DELETE /v3/subusers/{username} removes the subuser

// fakeSubuser is the server-side record kept by the mock.
type fakeSubuser struct {
	ID       int64
	Username string
	Email    string
	Disabled bool
	Region   string
}

// newMockSendGrid returns an httptest.Server that emulates the subset of the
// SendGrid Subusers API used by the resource. It is safe for the single-threaded
// access pattern of the test provider, guarded by a mutex for safety.
func newMockSendGrid(t *testing.T) *httptest.Server {
	t.Helper()

	var mu sync.Mutex
	store := map[string]*fakeSubuser{}
	var nextID int64 = 25000000

	mux := http.NewServeMux()

	// Create + List share the /v3/subusers path (POST vs GET).
	mux.HandleFunc("/v3/subusers", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.Method {
		case http.MethodPost:
			var body struct {
				Username string   `json:"username"`
				Email    string   `json:"email"`
				Password string   `json:"password"`
				IPs      []string `json:"ips"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid body", "")
				return
			}
			// Emulate SendGrid's password rule: at least one letter and one number.
			if !hasLetterAndNumber(body.Password) {
				writeErr(w, http.StatusBadRequest,
					"your password must contain at least one character and one number", "password")
				return
			}
			if _, exists := store[body.Username]; exists {
				writeErr(w, http.StatusBadRequest, "username already exists", "username")
				return
			}
			nextID++
			store[body.Username] = &fakeSubuser{
				ID:       nextID,
				Username: body.Username,
				Email:    body.Email,
				Disabled: false,
				Region:   "global",
			}
			w.WriteHeader(http.StatusCreated)
			// Documented Create response shape: user_id (not id), no ips.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"username": body.Username,
				"user_id":  nextID,
				"email":    body.Email,
				"credit_allocation": map[string]any{
					"type": "unlimited",
				},
			})

		case http.MethodGet:
			username := r.URL.Query().Get("username")
			out := []map[string]any{}
			if su, ok := store[username]; ok {
				out = append(out, map[string]any{
					"id":       su.ID,
					"username": su.Username,
					"email":    su.Email,
					"disabled": su.Disabled,
					"region":   su.Region,
				})
			}
			_ = json.NewEncoder(w).Encode(out)

		default:
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})

	// PATCH/DELETE /v3/subusers/{username}
	mux.HandleFunc("/v3/subusers/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		username := strings.TrimPrefix(r.URL.Path, "/v3/subusers/")
		su, ok := store[username]

		switch r.Method {
		case http.MethodPatch:
			if !ok {
				writeErr(w, http.StatusNotFound, "subuser not found", "")
				return
			}
			var body struct {
				Disabled bool `json:"disabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid body", "")
				return
			}
			su.Disabled = body.Disabled
			w.WriteHeader(http.StatusNoContent)

		case http.MethodDelete:
			// Delete is idempotent-ish; return 204 whether or not it existed.
			delete(store, username)
			w.WriteHeader(http.StatusNoContent)

		default:
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})

	return httptest.NewServer(mux)
}

func writeErr(w http.ResponseWriter, status int, message, field string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]any{
			{"message": message, "field": field},
		},
	})
}

func hasLetterAndNumber(s string) bool {
	var hasLetter, hasNumber bool
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			hasNumber = true
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			hasLetter = true
		}
	}
	return hasLetter && hasNumber
}

// providerConfig points the provider at the mock server via base_url and sets a
// dummy api_key so no real credentials are required.
func mockProviderConfig(baseURL string) string {
	return fmt.Sprintf(`
provider "sendgrid" {
  base_url = %q
  api_key  = "test-key"
}
`, baseURL)
}

// TestSubuserResource_mock_CRUD covers create -> readback -> disable toggle -> delete
// entirely against the mock server (no TF_ACC required).
func TestSubuserResource_mock_CRUD(t *testing.T) {
	srv := newMockSendGrid(t)
	defer srv.Close()

	config := func(disabled bool) string {
		return mockProviderConfig(srv.URL) + fmt.Sprintf(`
resource "sendgrid_subuser" "test" {
  username = "acctest-mock.example"
  email    = "acctest-mock@example.com"
  password = "abc12345"
  ips      = ["192.0.2.10"]
  disabled = %t
}
`, disabled)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testacc.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create (enabled)
			{
				Config: config(false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("sendgrid_subuser.test", "id"),
					resource.TestCheckResourceAttr("sendgrid_subuser.test", "username", "acctest-mock.example"),
					resource.TestCheckResourceAttr("sendgrid_subuser.test", "email", "acctest-mock@example.com"),
					resource.TestCheckResourceAttr("sendgrid_subuser.test", "disabled", "false"),
					resource.TestCheckResourceAttr("sendgrid_subuser.test", "region", "global"),
				),
			},
			// Toggle disabled true
			{
				Config: config(true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("sendgrid_subuser.test", "disabled", "true"),
				),
			},
			// Toggle back to false (round-trip)
			{
				Config: config(false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("sendgrid_subuser.test", "disabled", "false"),
				),
			},
		},
	})
}

// TestSubuserResource_mock_InvalidPassword verifies the 400 error path
// (password without a digit) surfaces as a Terraform error.
func TestSubuserResource_mock_InvalidPassword(t *testing.T) {
	srv := newMockSendGrid(t)
	defer srv.Close()

	config := mockProviderConfig(srv.URL) + `
resource "sendgrid_subuser" "bad" {
  username = "acctest-badpw.example"
  email    = "acctest-badpw@example.com"
  password = "onlyletters"
  ips      = ["192.0.2.10"]
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testacc.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				// The provider surfaces the API's error message in the
				// diagnostic summary, so we can assert on the password reason
				// specifically rather than the generic failure line.
				ExpectError: regexp.MustCompile(`password must contain at least one character and one number`),
			},
		},
	})
}
