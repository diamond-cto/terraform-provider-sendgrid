package provider

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
)

func TestProvider_Metadata_TypeName(t *testing.T) {
	p := New()
	var resp provider.MetadataResponse
	p.Metadata(context.Background(), provider.MetadataRequest{}, &resp)
	if resp.TypeName != "sendgrid" {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, "sendgrid")
	}
}

func TestProvider_Schema_HasAttributes(t *testing.T) {
	p := &SendGridProvider{}
	var resp provider.SchemaResponse
	p.Schema(context.Background(), provider.SchemaRequest{}, &resp)

	s := resp.Schema
	if s.MarkdownDescription == "" {
		t.Fatal("Schema.MarkdownDescription should not be empty")
	}

	baseURLAttr, ok := s.Attributes["base_url"]
	if !ok {
		t.Fatal(`Schema.Attributes["base_url"] missing`)
	}
	if a, ok := baseURLAttr.(providerschema.StringAttribute); !ok || !a.Optional {
		t.Fatal(`base_url must be Optional StringAttribute`)
	}

	apiKeyAttr, ok := s.Attributes["api_key"]
	if !ok {
		t.Fatal(`Schema.Attributes["api_key"] missing`)
	}
	if a, ok := apiKeyAttr.(providerschema.StringAttribute); !ok || !a.Optional || !a.Sensitive {
		t.Fatal(`api_key must be Optional & Sensitive StringAttribute`)
	}
}

func TestProvider_Configure_EnvOnly(t *testing.T) {
	// Arrange: no config; only env
	const wantBase = defaultBaseURL
	const wantKey = "test-key-from-env"

	orig := os.Getenv("SENDGRID_API_KEY")
	t.Cleanup(func() { _ = os.Setenv("SENDGRID_API_KEY", orig) })
	_ = os.Setenv("SENDGRID_API_KEY", wantKey)

	p := &SendGridProvider{}

	// Act
	var resp provider.ConfigureResponse
	p.Configure(context.Background(), provider.ConfigureRequest{}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Configure returned diagnostics: %v", resp.Diagnostics)
	}

	// Assert: client injected into DS/Resource contexts
	dsClient, ok := resp.DataSourceData.(*Client)
	if !ok {
		t.Fatal("DataSourceData is not *Client")
	}
	rsClient, ok := resp.ResourceData.(*Client)
	if !ok {
		t.Fatal("ResourceData is not *Client")
	}

	if dsClient.BaseURL != wantBase || rsClient.BaseURL != wantBase {
		t.Fatalf("BaseURL = %q/%q, want %q", dsClient.BaseURL, rsClient.BaseURL, wantBase)
	}
	if dsClient.APIKey != wantKey || rsClient.APIKey != wantKey {
		t.Fatalf("APIKey = %q/%q, want %q", dsClient.APIKey, rsClient.APIKey, wantKey)
	}
}

func TestProvider_FactoryLists_NotEmpty(t *testing.T) {
	p := &SendGridProvider{}

	if len(p.DataSources(context.Background())) == 0 {
		t.Fatal("DataSources() must not be empty")
	}
	if len(p.Resources(context.Background())) == 0 {
		t.Fatal("Resources() must not be empty")
	}
}
