package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var _ datasource.DataSource = (*subusersDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*subusersDataSource)(nil)

func NewSubusersDataSource() datasource.DataSource { return &subusersDataSource{} }

type subusersDataSource struct {
	client *Client
}

// Input model
//
// All fields optional; request is built only with provided values.
// See: https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/list-all-subusers
//
// GET /v3/subusers?username&limit&offset&region&include_region

type subusersDataSourceModel struct {
	Username      types.String `tfsdk:"username"`
	Limit         types.Int64  `tfsdk:"limit"`
	Offset        types.Int64  `tfsdk:"offset"`
	Region        types.String `tfsdk:"region"`         // all|global|eu
	IncludeRegion types.Bool   `tfsdk:"include_region"` // when true, API returns `region` per item

	Subusers types.List `tfsdk:"subusers"` // list of nested objects
}

// Response item from /v3/subusers
// When include_region=true, Region is present; otherwise it may be omitted.

type subuserAPI struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Disabled bool   `json:"disabled"`
	Region   string `json:"region,omitempty"`
}

func (d *subusersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_subusers"
}

func (d *subusersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "List SendGrid subusers via `/v3/subusers`. Optionally filter by `username`, `limit`, `offset`, and `region`. If `include_region` is true, each element includes a `region`.",
		Attributes: map[string]schema.Attribute{
			"username": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Filter by username (exact match).",
			},
			"limit": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Maximum number of results to return. If omitted, SendGrid defaults (typically 100).",
			},
			"offset": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Number of results to skip (pagination offset).",
			},
			"region": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Region filter: one of `all`, `global`, or `eu`.",
			},
			"include_region": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "If true, API includes `region` for each subuser in the response.",
			},
			"subusers": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "List of subusers.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Computed:            true,
							MarkdownDescription: "Subuser ID.",
						},
						"username": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Subuser username.",
						},
						"email": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Subuser email.",
						},
						"disabled": schema.BoolAttribute{
							Computed:            true,
							MarkdownDescription: "Whether the subuser is disabled.",
						},
						"region": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Region string when requested with include_region=true (may be empty otherwise).",
						},
					},
				},
			},
		},
	}
}

func (d *subusersDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	// Use the provider-configured client/config only. No independent fallbacks here.
	if req.ProviderData == nil {
		return
	}

	// Expect the provider to pass *Client. If the type is unexpected, surface a helpful error.
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = client
}

func (d *subusersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config subusersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil || d.client.APIKey == "" {
		resp.Diagnostics.AddError("Unconfigured provider", "The provider client was not configured or API key is empty. Ensure the provider is configured correctly.")
		return
	}

	// Build URL: {base}/v3/subusers
	u, err := url.Parse(d.client.BaseURL)
	if err != nil {
		resp.Diagnostics.AddError("Invalid base URL", err.Error())
		return
	}
	u.Path = "/v3/subusers"
	q := u.Query()

	if !config.Username.IsNull() && !config.Username.IsUnknown() {
		q.Set("username", config.Username.ValueString())
	}
	if !config.Limit.IsNull() && !config.Limit.IsUnknown() {
		q.Set("limit", strconv.FormatInt(config.Limit.ValueInt64(), 10))
	}
	if !config.Offset.IsNull() && !config.Offset.IsUnknown() {
		q.Set("offset", strconv.FormatInt(config.Offset.ValueInt64(), 10))
	}
	if !config.Region.IsNull() && !config.Region.IsUnknown() {
		q.Set("region", config.Region.ValueString())
	}
	if !config.IncludeRegion.IsNull() && !config.IncludeRegion.IsUnknown() {
		q.Set("include_region", fmt.Sprintf("%t", config.IncludeRegion.ValueBool()))
	}
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, "GET /v3/subusers", map[string]any{"url": u.String()})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		resp.Diagnostics.AddError("Building request failed", err.Error())
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+d.client.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	hc := &http.Client{}
	httpResp, err := hc.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("Request failed", err.Error())
		return
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	if httpResp.StatusCode != http.StatusOK {
		resp.Diagnostics.AddError("Unexpected status code", fmt.Sprintf("GET /v3/subusers returned %d", httpResp.StatusCode))
		return
	}

	var items []subuserAPI
	if err := json.NewDecoder(httpResp.Body).Decode(&items); err != nil {
		resp.Diagnostics.AddError("Decoding response failed", err.Error())
		return
	}

	// Map to state list
	// Build element type definition
	elemAttrTypes := map[string]attr.Type{
		"id":       types.Int64Type,
		"username": types.StringType,
		"email":    types.StringType,
		"disabled": types.BoolType,
		"region":   types.StringType,
	}

	elemType := types.ObjectType{AttrTypes: elemAttrTypes}
	elems := make([]types.Object, 0, len(items))
	for _, it := range items {
		// ensure region is empty when not provided
		obj, objDiags := types.ObjectValue(elemAttrTypes, map[string]attr.Value{
			"id":       types.Int64Value(it.ID),
			"username": types.StringValue(it.Username),
			"email":    types.StringValue(it.Email),
			"disabled": types.BoolValue(it.Disabled),
			"region": func() attr.Value {
				if it.Region == "" {
					return types.StringNull()
				}
				return types.StringValue(it.Region)
			}(),
		})
		resp.Diagnostics.Append(objDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
		elems = append(elems, obj)
	}

	listVal, listDiags := types.ListValueFrom(ctx, elemType, elems)
	resp.Diagnostics.Append(listDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := subusersDataSourceModel{
		Username:      config.Username,
		Limit:         config.Limit,
		Offset:        config.Offset,
		Region:        config.Region,
		IncludeRegion: config.IncludeRegion,
		Subusers:      listVal,
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
