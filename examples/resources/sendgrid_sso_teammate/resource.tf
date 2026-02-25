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
# Restricted main-account scopes + per-Subuser access
############################
resource "sendgrid_sso_teammate" "readonly" {
  email      = "readonly@example.com"
  first_name = "Read"
  last_name  = "Only"

  # Main-account permissions (only effective when is_admin = false)
  is_admin = false
  scopes = [
    "user.account.read",
    "user.profile.read",
    "stats.read",
  ]

  # Set to true to manage permissions per Subuser
  has_restricted_subuser_access = true

  # Access settings for each assigned Subuser
  subuser_access {
    id              = "1234567"    # ← Replace with the ID of an existing Subuser (as string)
    permission_type = "restricted" # "restricted" | "admin"
    scopes = [                     # For "restricted", list the allowed scopes
      "messages.read",
      "stats.read",
      "user.account.read",
      "user.username.read",
      "tracking_settings.read",
    ]
  }
}

############################
# Mixed: multiple Subusers (restricted + admin)
############################
resource "sendgrid_sso_teammate" "ops" {
  email = "ops@example.com"

  is_admin = false
  scopes = [
    "mail.send",
    "stats.read",
  ]

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