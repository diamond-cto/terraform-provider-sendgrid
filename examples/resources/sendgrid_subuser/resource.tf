############################
# Provider configuration
############################
provider "sendgrid" {
  # If api_key is not specified, the environment variable SENDGRID_API_KEY is used
  # base_url defaults to US (https://api.sendgrid.com). For EU, enable the following:
  # base_url = "https://api.eu.sendgrid.com"
}

############################
# Basic subuser
############################
resource "sendgrid_subuser" "example" {
  username = "my_subuser"
  email    = "my_subuser@example.com"
  password = var.subuser_password # keep secrets out of the configuration

  # IPs assigned at creation. Ongoing IP management is out of scope for this
  # resource; changing this list forces the subuser to be recreated.
  ips = [
    "192.0.2.10",
  ]
}

############################
# Disabled subuser
############################
resource "sendgrid_subuser" "disabled_example" {
  username = "paused_subuser"
  email    = "paused_subuser@example.com"
  password = var.subuser_password

  ips = [
    "192.0.2.10",
  ]

  disabled = true
}

variable "subuser_password" {
  type      = string
  sensitive = true
}

output "example_subuser_id" {
  value = sendgrid_subuser.example.id
}
