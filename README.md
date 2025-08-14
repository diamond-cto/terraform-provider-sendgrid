# Terraform Provider for SendGrid

A Terraform provider to manage [SendGrid](https://sendgrid.com/) resources via the official API.

## Features

- Manage **SSO Teammates** (`/v3/sso/teammates`)
- Manage **Teammate Subuser Access** (`/v3/teammates/{username}/subuser_access`)
- List **Subusers** (`/v3/subusers`)
- Data sources for retrieving teammate and subuser information

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.6
- Go >= 1.21 (for building the provider)
- A valid [SendGrid API Key](https://app.sendgrid.com/settings/api_keys)

## Installation

### Local Development

1. Build and install the provider binary:
   ```bash
   go install .
   ```
