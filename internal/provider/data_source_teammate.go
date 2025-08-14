package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/sendgrid/sendgrid-go"
)

// Ensure implementation satisfies the expected interfaces.
var _ datasource.DataSource = (*TeammateDataSource)(nil)

// TeammateDataSource implements the sendgrid_teammate data source.
type TeammateDataSource struct {
	client *Client
}

// NewTeammateDataSource returns a new instance of the teammate data source.
func NewTeammateDataSource() datasource.DataSource {
	return &TeammateDataSource{}
}

// teammateModel maps data source schema data.
type teammateModel struct {
	OnBehalfOf types.String `tfsdk:"on_behalf_of"`
	Username   types.String `tfsdk:"username"`
	Email      types.String `tfsdk:"email"`
	FirstName  types.String `tfsdk:"first_name"`
	LastName   types.String `tfsdk:"last_name"`
	UserType   types.String `tfsdk:"user_type"`
	IsAdmin    types.Bool   `tfsdk:"is_admin"`
	Scopes     types.Set    `tfsdk:"scopes"`
	Phone      types.String `tfsdk:"phone"`
	Website    types.String `tfsdk:"website"`
	Company    types.String `tfsdk:"company"`
	Address    types.String `tfsdk:"address"`
	Address2   types.String `tfsdk:"address2"`
	City       types.String `tfsdk:"city"`
	State      types.String `tfsdk:"state"`
	Zip        types.String `tfsdk:"zip"`
	Country    types.String `tfsdk:"country"`
}

// Metadata sets the data source type name.
func (d *TeammateDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_teammate"
}

// Schema defines the data source schema.
func (d *TeammateDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lookup a SendGrid teammate by username and return details.",
		Attributes: map[string]schema.Attribute{
			"on_behalf_of": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Parent account header to impersonate a Subuser: sets the HTTP header `on-behalf-of` to the given subuser username.",
			},
			"username": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Teammate username.",
			},
			"email": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Teammate email address.",
			},
			"first_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Teammate first name.",
			},
			"last_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Teammate last name.",
			},
			"user_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "User type: one of `owner`, `admin`, or `teammate`.",
			},
			"phone": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Teammate phone number (optional).",
			},
			"website": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Teammate website (optional).",
			},
			"company": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Company name (optional).",
			},
			"address": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Street address (optional).",
			},
			"address2": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Additional address line (optional).",
			},
			"city": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "City (optional).",
			},
			"state": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "State/Province (optional).",
			},
			"zip": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ZIP/Postal code (optional).",
			},
			"country": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Country (optional).",
			},
			"is_admin": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the teammate has admin permissions.",
			},
			"scopes": schema.SetAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of granted scopes for the teammate.",
			},
		},
	}
}

// Configure receives provider configured client.
func (d *TeammateDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*Client)
	if !ok || c == nil {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *Client, got: %T. Please report this issue to the provider maintainers.", req.ProviderData),
		)
		return
	}

	d.client = c
}

// Read fetches teammate details and sets the state.
func (d *TeammateDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data teammateModel

	diags := req.Config.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Username.IsUnknown() || data.Username.IsNull() {
		resp.Diagnostics.AddError("Missing username", "The required attribute 'username' is missing or unknown.")
		return
	}

	var onBehalf string
	if !data.OnBehalfOf.IsNull() && !data.OnBehalfOf.IsUnknown() {
		onBehalf = data.OnBehalfOf.ValueString()
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Unconfigured provider", "The provider client was not configured.")
		return
	}

	username := data.Username.ValueString()

	// Build request using sendgrid-go with provider-configured BaseURL (EU/US support).
	request := sendgrid.GetRequest(d.client.APIKey, "/v3/teammates/"+username, d.client.BaseURL)
	request.Method = "GET"

	if onBehalf != "" {
		if request.Headers == nil {
			request.Headers = make(map[string]string)
		}
		request.Headers["on-behalf-of"] = onBehalf
	}

	sgResp, err := sendgrid.API(request)
	if err != nil {
		resp.Diagnostics.AddError("SendGrid API request failed", err.Error())
		return
	}

	if sgResp.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			"SendGrid API error",
			fmt.Sprintf("HTTP %d while fetching teammate '%s': %s", sgResp.StatusCode, username, sgResp.Body),
		)
		return
	}

	var payload struct {
		Username  string   `json:"username"`
		FirstName string   `json:"first_name"`
		LastName  string   `json:"last_name"`
		Email     string   `json:"email"`
		Scopes    []string `json:"scopes"`
		UserType  string   `json:"user_type"`
		IsAdmin   bool     `json:"is_admin"`
		Phone     string   `json:"phone"`
		Website   string   `json:"website"`
		Company   string   `json:"company"`
		Address   string   `json:"address"`
		Address2  string   `json:"address2"`
		City      string   `json:"city"`
		State     string   `json:"state"`
		Zip       string   `json:"zip"`
		Country   string   `json:"country"`
	}
	if err := json.Unmarshal([]byte(sgResp.Body), &payload); err != nil {
		resp.Diagnostics.AddError("Failed to parse API response", fmt.Sprintf("Unable to parse JSON body: %v", err))
		return
	}

	if payload.Username == "" {
		resp.Diagnostics.AddError("Teammate not found", "The API returned an empty username; verify the input 'username'.")
		return
	}

	data.Email = types.StringValue(payload.Email)
	data.FirstName = types.StringValue(payload.FirstName)
	data.LastName = types.StringValue(payload.LastName)
	data.UserType = types.StringValue(payload.UserType)
	data.Phone = types.StringValue(payload.Phone)
	data.Website = types.StringValue(payload.Website)
	data.Company = types.StringValue(payload.Company)
	data.Address = types.StringValue(payload.Address)
	data.Address2 = types.StringValue(payload.Address2)
	data.City = types.StringValue(payload.City)
	data.State = types.StringValue(payload.State)
	data.Zip = types.StringValue(payload.Zip)
	data.Country = types.StringValue(payload.Country)
	data.IsAdmin = types.BoolValue(payload.IsAdmin)

	// Convert scopes slice to Terraform types.Set
	scopeVals := make([]attr.Value, 0, len(payload.Scopes))
	for _, s := range payload.Scopes {
		scopeVals = append(scopeVals, types.StringValue(s))
	}
	setVal, diagSet := types.SetValue(types.StringType, scopeVals)
	resp.Diagnostics.Append(diagSet...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Scopes = setVal

	if diags := resp.State.Set(ctx, &data); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
}
