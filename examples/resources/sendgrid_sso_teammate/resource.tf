############################
# Provider configuration
############################
provider "sendgrid" {
  # If api_key is not specified, the environment variable SENDGRID_API_KEY is used
  # base_url defaults to US (https://api.sendgrid.com). For EU, enable the following:
  # base_url = "https://api.eu.sendgrid.com"
}

############################
# Admin: full main-account access
############################
resource "sendgrid_sso_teammate" "admin" {
  email      = "admin@example.com"
  first_name = "Admin"
  last_name  = "User"

  # Grant full admin access to the main account
  is_admin = true

  has_restricted_subuser_access = false
}

############################
# Main-account scopes only (no per-Subuser restrictions)
# Note: scopes and has_restricted_subuser_access = true are mutually exclusive
############################
resource "sendgrid_sso_teammate" "readonly" {
  email      = "readonly@example.com"
  first_name = "Read"
  last_name  = "Only"

  is_admin = false
  scopes = [
    "user.account.read",
    "user.profile.read",
    "stats.read",
  ]

  has_restricted_subuser_access = false
}

############################
# Per-Subuser restricted access (without main-account scopes)
############################
resource "sendgrid_sso_teammate" "ops" {
  email = "ops@example.com"

  is_admin = false

  has_restricted_subuser_access = true

  subuser_access {
    id              = "1111111"
    permission_type = "restricted"
    scopes = [
      "messages.read",
      "stats.read",
    ]
  }

  subuser_access {
    id              = "2222222"
    permission_type = "admin" # For "admin", scopes are ignored
    scopes          = []
  }
}

############################
# Useful outputs for testing
############################
output "admin_email" {
  value = sendgrid_sso_teammate.admin.email
}

output "readonly_email" {
  value = sendgrid_sso_teammate.readonly.email
}

output "ops_email" {
  value = sendgrid_sso_teammate.ops.email
}