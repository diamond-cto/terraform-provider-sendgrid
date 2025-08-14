package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/sendgrid/sendgrid-go"
)

// Ensure implementation satisfies the expected interfaces.
var _ datasource.DataSource = (*TeammateSubuserAccessDataSource)(nil)

// TeammateSubuserAccessDataSource implements the sendgrid_teammate_subuser_access data source.
type TeammateSubuserAccessDataSource struct {
	client *Client
}

// NewTeammateSubuserAccessDataSource returns a new instance of the teammate_subuser_access data source.
func NewTeammateSubuserAccessDataSource() datasource.DataSource {
	return &TeammateSubuserAccessDataSource{}
}

// teammateSubuserAccessModel maps data source schema data.
type teammateSubuserAccessModel struct {
	TeammateName               types.String         `tfsdk:"teammate_name"`
	Limit                      types.Int64          `tfsdk:"limit"`
	AfterSubuserID             types.Int64          `tfsdk:"after_subuser_id"`
	Username                   types.String         `tfsdk:"username"`
	HasRestrictedSubuserAccess types.Bool           `tfsdk:"has_restricted_subuser_access"`
	SubuserAccess              []subuserAccessModel `tfsdk:"subuser_access"`
	NextLimit                  types.Int64          `tfsdk:"next_limit"`
	NextAfterSubuserID         types.Int64          `tfsdk:"next_after_subuser_id"`
	NextUsername               types.String         `tfsdk:"next_username"`
}

type subuserAccessModel struct {
	ID             types.Int64  `tfsdk:"id"`
	Username       types.String `tfsdk:"username"`
	Email          types.String `tfsdk:"email"`
	Disabled       types.Bool   `tfsdk:"disabled"`
	PermissionType types.String `tfsdk:"permission_type"`
	Scopes         types.Set    `tfsdk:"scopes"`
}

// Metadata sets the data source type name.
func (d *TeammateSubuserAccessDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_teammate_subuser_access"
}

// Schema defines the data source schema.
func (d *TeammateSubuserAccessDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Retrieve subuser access details for a SendGrid teammate.",
		Attributes: map[string]schema.Attribute{
			"teammate_name": schema.StringAttribute{
				MarkdownDescription: "Teammate username.",
				Required:            true,
			},
			"limit": schema.Int64Attribute{
				MarkdownDescription: "Maximum number of results to return.",
				Optional:            true,
			},
			"after_subuser_id": schema.Int64Attribute{
				MarkdownDescription: "Pagination cursor: fetch results after this subuser ID.",
				Optional:            true,
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "Filter results by subuser username (query parameter: `username`).",
				Optional:            true,
			},
			"has_restricted_subuser_access": schema.BoolAttribute{
				MarkdownDescription: "Whether the teammate has restricted subuser access.",
				Computed:            true,
			},
			"subuser_access": schema.ListNestedAttribute{
				MarkdownDescription: "List of subuser access entries.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							MarkdownDescription: "Subuser access ID.",
							Computed:            true,
						},
						"username": schema.StringAttribute{
							MarkdownDescription: "Subuser username.",
							Computed:            true,
						},
						"email": schema.StringAttribute{
							MarkdownDescription: "Subuser email address.",
							Computed:            true,
						},
						"disabled": schema.BoolAttribute{
							MarkdownDescription: "Whether the subuser is disabled.",
							Computed:            true,
						},
						"permission_type": schema.StringAttribute{
							MarkdownDescription: "Permission type: 'admin' or 'restricted'.",
							Computed:            true,
						},
						"scopes": schema.SetAttribute{
							ElementType:         types.StringType,
							MarkdownDescription: "List of granted scopes for the subuser.",
							Computed:            true,
						},
					},
				},
			},
			"next_limit": schema.Int64Attribute{
				MarkdownDescription: "Next page limit parameter for pagination.",
				Computed:            true,
			},
			"next_after_subuser_id": schema.Int64Attribute{
				MarkdownDescription: "Next page after_subuser_id parameter for pagination.",
				Computed:            true,
			},
			"next_username": schema.StringAttribute{
				MarkdownDescription: "Next page username parameter for pagination (echo of query `username`).",
				Computed:            true,
			},
		},
	}
}

// Configure receives provider configured client.
func (d *TeammateSubuserAccessDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches teammate subuser access details and sets the state.
func (d *TeammateSubuserAccessDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state teammateSubuserAccessModel

	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.TeammateName.IsUnknown() || state.TeammateName.IsNull() {
		resp.Diagnostics.AddError("Missing teammate_name", "The required attribute 'teammate_name' is missing or unknown.")
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Unconfigured provider", "The provider client was not configured.")
		return
	}

	teammateName := state.TeammateName.ValueString()

	// Build request using sendgrid-go with provider-configured BaseURL (EU/US support).
	path := "/v3/teammates/" + teammateName + "/subuser_access"
	request := sendgrid.GetRequest(d.client.APIKey, path, d.client.BaseURL)
	request.Method = "GET"

	// Add query parameters if provided
	queryParams := make(map[string]string)
	if !state.Limit.IsNull() && !state.Limit.IsUnknown() {
		queryParams["limit"] = strconv.FormatInt(state.Limit.ValueInt64(), 10)
	}
	if !state.AfterSubuserID.IsNull() && !state.AfterSubuserID.IsUnknown() {
		queryParams["after_subuser_id"] = strconv.FormatInt(state.AfterSubuserID.ValueInt64(), 10)
	}
	if !state.Username.IsNull() && !state.Username.IsUnknown() {
		queryParams["username"] = state.Username.ValueString()
	}
	if len(queryParams) > 0 {
		q := request.QueryParams
		if q == nil {
			q = make(map[string]string)
		}
		for k, v := range queryParams {
			q[k] = v
		}
		request.QueryParams = q
	}

	sgResp, err := sendgrid.API(request)
	if err != nil {
		resp.Diagnostics.AddError("SendGrid API request failed", err.Error())
		return
	}

	if sgResp.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			"SendGrid API error",
			fmt.Sprintf("HTTP %d while fetching teammate subuser access '%s': %s", sgResp.StatusCode, teammateName, sgResp.Body),
		)
		return
	}

	var payload struct {
		HasRestrictedSubuserAccess bool `json:"has_restricted_subuser_access"`
		SubuserAccess              []struct {
			ID             int64    `json:"id"`
			Username       string   `json:"username"`
			Email          string   `json:"email"`
			Disabled       bool     `json:"disabled"`
			PermissionType string   `json:"permission_type"`
			Scopes         []string `json:"scopes"`
		} `json:"subuser_access"`
		Metadata struct {
			NextParams struct {
				Limit          int64  `json:"limit"`
				AfterSubuserID int64  `json:"after_subuser_id"`
				Username       string `json:"username"`
			} `json:"next_params"`
		} `json:"_metadata"`
	}

	if err := json.Unmarshal([]byte(sgResp.Body), &payload); err != nil {
		resp.Diagnostics.AddError("Failed to parse API response", fmt.Sprintf("Unable to parse JSON body: %v", err))
		return
	}

	state.HasRestrictedSubuserAccess = types.BoolValue(payload.HasRestrictedSubuserAccess)

	subuserAccessList := make([]subuserAccessModel, 0, len(payload.SubuserAccess))
	for _, item := range payload.SubuserAccess {
		scopeVals := make([]attr.Value, 0, len(item.Scopes))
		for _, s := range item.Scopes {
			scopeVals = append(scopeVals, types.StringValue(s))
		}
		setVal, diagSet := types.SetValue(types.StringType, scopeVals)
		resp.Diagnostics.Append(diagSet...)
		if resp.Diagnostics.HasError() {
			return
		}
		subuserAccessList = append(subuserAccessList, subuserAccessModel{
			ID:             types.Int64Value(item.ID),
			Username:       types.StringValue(item.Username),
			Email:          types.StringValue(item.Email),
			Disabled:       types.BoolValue(item.Disabled),
			PermissionType: types.StringValue(item.PermissionType),
			Scopes:         setVal,
		})
	}
	state.SubuserAccess = subuserAccessList

	// Pagination hints
	state.NextLimit = types.Int64Value(payload.Metadata.NextParams.Limit)
	state.NextAfterSubuserID = types.Int64Value(payload.Metadata.NextParams.AfterSubuserID)
	state.NextUsername = types.StringValue(payload.Metadata.NextParams.Username)

	if diags := resp.State.Set(ctx, &state); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
}
