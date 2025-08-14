package provider

import (
	"context"
	"os"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const defaultBaseURL = "https://api.sendgrid.com"

// Ensure implementation satisfies the expected interfaces.
var _ provider.Provider = (*SendGridProvider)(nil)

// New returns a new instance of the SendGrid provider.
func New() provider.Provider { return &SendGridProvider{} }

// SendGridProvider implements the Terraform provider interface.
type SendGridProvider struct{}

// Metadata sets the provider type name.
func (p *SendGridProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "sendgrid"
}

// Schema defines provider-level configuration.
func (p *SendGridProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerschema.Schema{
		MarkdownDescription: "Terraform provider for SendGrid.",
		Attributes: map[string]providerschema.Attribute{
			"base_url": providerschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Base URL for the SendGrid API. Defaults to https://api.sendgrid.com if unset.",
			},
			"api_key": providerschema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "SendGrid API key. If unset, the SENDGRID_API_KEY environment variable is used.",
			},
		},
	}
}

// DataSources returns no data sources for now.
func (p *SendGridProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewTeammateDataSource,
		NewTeammateSubuserAccessDataSource,
		NewSubusersDataSource,
	}
}

// Resources returns no resources for now.
func (p *SendGridProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSSOTeammateResource,
	}
}

// providerModel holds provider configuration fields.
type providerModel struct {
	BaseURL types.String `tfsdk:"base_url"`
	APIKey  types.String `tfsdk:"api_key"`
}

// Client is a minimal API client placeholder shared with resources/data sources.
type Client struct {
	BaseURL string
	APIKey  string
}

// Configure creates a client from configuration and environment variables.
func (p *SendGridProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel

	// In some unit-test or framework call paths, req.Config can be a zero-value
	// which would cause tfsdk.Config.Get to panic. Guard and treat it as empty.
	if !reflect.ValueOf(req.Config).IsZero() {
		diags := req.Config.Get(ctx, &cfg)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Resolve base URL.
	baseURL := defaultBaseURL
	if !cfg.BaseURL.IsNull() && !cfg.BaseURL.IsUnknown() {
		if v := cfg.BaseURL.ValueString(); v != "" {
			baseURL = v
		}
	}

	// Resolve API key from config or environment.
	apiKey := ""
	if !cfg.APIKey.IsNull() && !cfg.APIKey.IsUnknown() {
		apiKey = cfg.APIKey.ValueString()
	}
	if apiKey == "" {
		apiKey = os.Getenv("SENDGRID_API_KEY")
	}

	client := &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}
