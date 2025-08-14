############################
# Provider configuration
############################
provider "sendgrid" {
  # api_key は未指定時、環境変数 SENDGRID_API_KEY を使用します
  # base_url は省略時 US (https://api.sendgrid.com)。EU の場合は以下を有効化:
  # base_url = "https://api.eu.sendgrid.com"
}

############################
# Minimal: restricted read‑only on a single Subuser
############################
resource "sendgrid_sso_teammate" "readonly" {
  email      = "readonly@example.com"
  first_name = "Read"
  last_name  = "Only"

  # Subuser 単位で権限管理を行う場合は true
  has_restricted_subuser_access = true

  # 付与する Subuser ごとのアクセス設定
  subuser_access = [
    {
      id              = 1234567      # ← 既存 Subuser の ID に置き換え
      permission_type = "restricted" # "restricted" | "admin"
      scopes = [                     # restricted の場合は許可スコープを列挙
        "messages.read",
        "stats.read",
        "user.account.read",
        "user.username.read",
        "tracking_settings.read",
      ]
    }
  ]
}

############################
# Mixed: multiple Subusers (restricted + admin)
############################
resource "sendgrid_sso_teammate" "ops" {
  email = "ops@example.com"

  has_restricted_subuser_access = true

  subuser_access = [
    {
      id              = 1111111
      permission_type = "restricted"
      scopes = [
        "messages.read",
        "stats.read",
      ]
    },
    {
      id              = 2222222
      permission_type = "admin" # admin の場合、scopes は無視されます
      scopes          = []
    }
  ]
}

############################
# Useful outputs during testing
############################
output "readonly_email" {
  value = sendgrid_sso_teammate.readonly.email
}

output "ops_email" {
  value = sendgrid_sso_teammate.ops.email
}
