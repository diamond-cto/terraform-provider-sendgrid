package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/sendgrid/sendgrid-go"
)

// NOTE: This resource manages SSO Teammates via POST/PATCH /v3/sso/teammates and
// reads/deletes via GET/DELETE /v3/teammates/{username}.
// See:
// - Create: POST /v3/sso/teammates
// - Edit:   PATCH /v3/sso/teammates/{username}
// - Read:   GET   /v3/teammates/{username}
// - Delete: DELETE /v3/teammates/{username}
// Docs:
// https://www.twilio.com/docs/sendgrid/api-reference/single-sign-on-teammates/create-sso-teammate
// https://www.twilio.com/docs/sendgrid/api-reference/single-sign-on-teammates/edit-an-sso-teammate
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/retrieve-specific-teammate
// https://www.twilio.com/docs/sendgrid/api-reference/teammates/delete-teammate

var _ resource.Resource = (*SSOTeammateResource)(nil)
var _ resource.ResourceWithConfigure = (*SSOTeammateResource)(nil)

func NewSSOTeammateResource() resource.Resource { return &SSOTeammateResource{} }

type SSOTeammateResource struct{ client *Client }

type ssoTeammateModel struct {
	ID        types.String `tfsdk:"id"`
	Email     types.String `tfsdk:"email"`
	FirstName types.String `tfsdk:"first_name"`
	LastName  types.String `tfsdk:"last_name"`

	HasRestricted types.Bool   `tfsdk:"has_restricted_subuser_access"`
	SubuserAccess types.List   `tfsdk:"subuser_access"`
	Status        types.String `tfsdk:"status"`
}

type subuserAccessObject struct {
	ID             types.Int64  `tfsdk:"id"`
	PermissionType types.String `tfsdk:"permission_type"`
	Scopes         types.Set    `tfsdk:"scopes"`
}

func (r *SSOTeammateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sso_teammate"
}

func (r *SSOTeammateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pc, ok := req.ProviderData.(*Client)
	if !ok || pc == nil {
		resp.Diagnostics.AddError("Unexpected ProviderData",
			"Expected *Client, got something else")
		return
	}
	r.client = pc
}

func (r *SSOTeammateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a Twilio SendGrid SSO Teammate and optional per‑Subuser restricted access (scopes).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Resource identifier; same as email/username.",
			},
			"email": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Teammate email (also used as username for SSO).",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(3),
				},
			},
			"first_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Teammate first name.",
			},
			"last_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Teammate last name.",
			},
			"has_restricted_subuser_access": schema.BoolAttribute{
				Required:            true,
				MarkdownDescription: "Set true to configure per‑Subuser permissions with `subuser_access`.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Current teammate status returned by GET /v3/teammates/{username} (e.g., active, pending).",
			},
		},
		Blocks: map[string]schema.Block{
			"subuser_access": schema.ListNestedBlock{
				MarkdownDescription: "Per‑Subuser access when `has_restricted_subuser_access = true`. For `permission_type = restricted`, `scopes` must list allowed scopes.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Required:            true,
							MarkdownDescription: "Subuser ID.",
						},
						"permission_type": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "`restricted` or `admin`. When `restricted`, only `scopes` are granted.",
							Validators: []validator.String{
								stringvalidator.OneOf("restricted", "admin"),
							},
						},
						"scopes": schema.SetAttribute{
							ElementType:         types.StringType,
							Optional:            true,
							MarkdownDescription: "List of allowed scopes when `permission_type = restricted`. Ignored for `admin`.",
							PlanModifiers: []planmodifier.Set{
								setplanmodifier.UseStateForUnknown(),
							},
						},
					},
				},
			},
		},
	}
}

// ---------- API payloads ----------

type ssoCreatePayload struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`

	HasRestricted bool                 `json:"has_restricted_subuser_access"`
	SubuserAccess []subuserAccessEntry `json:"subuser_access,omitempty"`
}

type ssoPatchPayload struct {
	FirstName *string `json:"first_name,omitempty"`
	LastName  *string `json:"last_name,omitempty"`

	HasRestricted *bool                `json:"has_restricted_subuser_access,omitempty"`
	SubuserAccess []subuserAccessEntry `json:"subuser_access,omitempty"`
}

type subuserAccessEntry struct {
	ID             int64    `json:"id"`
	PermissionType string   `json:"permission_type"`
	Scopes         []string `json:"scopes,omitempty"`
}

type teammateGetResponse struct {
	Username  string `json:"username"`
	Email     string `json:"email"`
	Status    string `json:"status"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type teammateSubuserAccessResponse struct {
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

// ---------- CRUD ----------

func (r *SSOTeammateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Not configured", "Provider configuration is missing")
		return
	}

	var plan ssoTeammateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := ssoCreatePayload{
		Email:         plan.Email.ValueString(),
		FirstName:     plan.FirstName.ValueString(),
		LastName:      plan.LastName.ValueString(),
		HasRestricted: plan.HasRestricted.ValueBool(),
	}

	// Build subuser_access
	if !plan.SubuserAccess.IsNull() && !plan.SubuserAccess.IsUnknown() {
		var objs []subuserAccessObject
		resp.Diagnostics.Append(plan.SubuserAccess.ElementsAs(ctx, &objs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		for _, o := range objs {
			entry := subuserAccessEntry{
				ID:             o.ID.ValueInt64(),
				PermissionType: o.PermissionType.ValueString(),
			}
			if !o.Scopes.IsNull() && !o.Scopes.IsUnknown() {
				var scopes []string
				resp.Diagnostics.Append(o.Scopes.ElementsAs(ctx, &scopes, false)...)
				if resp.Diagnostics.HasError() {
					return
				}
				entry.Scopes = scopes
			}
			payload.SubuserAccess = append(payload.SubuserAccess, entry)
		}
	}

	b, _ := json.Marshal(payload)
	reqSG := sendgrid.GetRequest(r.client.APIKey, "/v3/sso/teammates", r.client.BaseURL)
	reqSG.Method = "POST"
	reqSG.Body = b

	sgResp, err := sendgrid.API(reqSG)
	if err != nil {
		resp.Diagnostics.AddError("SendGrid API error", err.Error())
		return
	}
	if sgResp.StatusCode >= 300 {
		resp.Diagnostics.AddError("Create SSO Teammate failed",
			fmt.Sprintf("status=%d body=%s", sgResp.StatusCode, sgResp.Body))
		return
	}

	// After create, read back teammate + subuser_access to ensure state is fully known
	username := plan.Email.ValueString()

	tflog.Debug(ctx, "Post-create GET /v3/teammates/{username}", map[string]any{"username": username})
	reqGet := sendgrid.GetRequest(r.client.APIKey, "/v3/teammates/"+username, r.client.BaseURL)
	reqGet.Method = "GET"
	getResp, err := sendgrid.API(reqGet)
	if err != nil {
		resp.Diagnostics.AddError("SendGrid API error (post-create read)", err.Error())
		return
	}
	if getResp.StatusCode >= 300 {
		resp.Diagnostics.AddError("Post-create read failed", fmt.Sprintf("status=%d body=%s", getResp.StatusCode, getResp.Body))
		return
	}
	var got teammateGetResponse
	if err := json.Unmarshal([]byte(getResp.Body), &got); err != nil {
		resp.Diagnostics.AddError("Parse error (post-create teammate)", fmt.Sprintf("unable to parse body: %v", err))
		return
	}

	// GET /v3/teammates/{username}/subuser_access with pagination
	var allEntries []subuserAccessEntry
	var hasRestricted bool
	var afterID int64 = 0
	for {
		tflog.Debug(ctx, "Post-create GET /v3/teammates/{username}/subuser_access", map[string]any{"username": username, "after_subuser_id": afterID})
		reqSA := sendgrid.GetRequest(r.client.APIKey, "/v3/teammates/"+username+"/subuser_access", r.client.BaseURL)
		reqSA.Method = "GET"
		if reqSA.QueryParams == nil {
			reqSA.QueryParams = make(map[string]string)
		}
		reqSA.QueryParams["limit"] = "100"
		if afterID > 0 {
			reqSA.QueryParams["after_subuser_id"] = strconv.FormatInt(afterID, 10)
		}

		saResp, err := sendgrid.API(reqSA)
		if err != nil {
			resp.Diagnostics.AddError("SendGrid API error (post-create subuser_access)", err.Error())
			return
		}
		if saResp.StatusCode >= 300 {
			resp.Diagnostics.AddError("Post-create subuser_access read failed", fmt.Sprintf("status=%d body=%s", saResp.StatusCode, saResp.Body))
			return
		}
		var sa teammateSubuserAccessResponse
		if err := json.Unmarshal([]byte(saResp.Body), &sa); err != nil {
			resp.Diagnostics.AddError("Parse error (post-create subuser_access)", fmt.Sprintf("unable to parse body: %v", err))
			return
		}
		hasRestricted = sa.HasRestrictedSubuserAccess
		for _, e := range sa.SubuserAccess {
			allEntries = append(allEntries, subuserAccessEntry{ID: e.ID, PermissionType: e.PermissionType, Scopes: e.Scopes})
		}
		if sa.Metadata.NextParams.AfterSubuserID == 0 {
			break
		}
		afterID = sa.Metadata.NextParams.AfterSubuserID
	}

	// map to state model
	// normalize names from API response
	if got.FirstName != "" {
		plan.FirstName = types.StringValue(got.FirstName)
	} else {
		plan.FirstName = types.StringNull()
	}
	if got.LastName != "" {
		plan.LastName = types.StringValue(got.LastName)
	} else {
		plan.LastName = types.StringNull()
	}
	plan.Status = types.StringValue(got.Status)
	plan.HasRestricted = types.BoolValue(hasRestricted)
	if len(allEntries) > 0 {
		objs := make([]subuserAccessObject, 0, len(allEntries))
		for _, e := range allEntries {
			o := subuserAccessObject{ID: types.Int64Value(e.ID), PermissionType: types.StringValue(e.PermissionType)}
			if len(e.Scopes) > 0 {
				setVals := make([]attr.Value, 0, len(e.Scopes))
				for _, s := range e.Scopes {
					setVals = append(setVals, types.StringValue(s))
				}
				o.Scopes, _ = types.SetValue(types.StringType, setVals)
			} else {
				o.Scopes = types.SetNull(types.StringType)
			}
			objs = append(objs, o)
		}
		lv, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: map[string]attr.Type{
			"id": types.Int64Type, "permission_type": types.StringType, "scopes": types.SetType{ElemType: types.StringType},
		}}, objs)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.SubuserAccess = lv
	} else {
		plan.SubuserAccess = types.ListNull(types.ObjectType{AttrTypes: map[string]attr.Type{
			"id": types.Int64Type, "permission_type": types.StringType, "scopes": types.SetType{ElemType: types.StringType},
		}})
	}
	plan.ID = types.StringValue(plan.Email.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SSOTeammateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Not configured", "Provider configuration is missing")
		return
	}
	var state ssoTeammateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	username := state.Email.ValueString()
	if username == "" && !state.ID.IsNull() && !state.ID.IsUnknown() {
		username = state.ID.ValueString()
	}
	if username == "" {
		resp.Diagnostics.AddError("Missing identifier", "Both email and id are empty; cannot read resource")
		return
	}
	reqSG := sendgrid.GetRequest(r.client.APIKey, "/v3/teammates/"+username, r.client.BaseURL)
	reqSG.Method = "GET"
	sgResp, err := sendgrid.API(reqSG)
	if err != nil {
		resp.Diagnostics.AddError("SendGrid API error", err.Error())
		return
	}
	if sgResp.StatusCode == 404 {
		// Treat as removed from remote
		resp.State.RemoveResource(ctx)
		return
	}
	if sgResp.StatusCode >= 300 {
		resp.Diagnostics.AddError("Read teammate failed",
			fmt.Sprintf("status=%d body=%s", sgResp.StatusCode, sgResp.Body))
		return
	}

	var got teammateGetResponse
	if err := json.Unmarshal([]byte(sgResp.Body), &got); err != nil {
		resp.Diagnostics.AddError("Parse error", fmt.Sprintf("unable to parse body: %v", err))
		return
	}

	// Fetch subuser access to fully populate state (useful for import and read-after-apply)
	var allEntries []subuserAccessEntry
	var hasRestricted bool
	var afterID int64 = 0
	for {
		reqSA := sendgrid.GetRequest(r.client.APIKey, "/v3/teammates/"+username+"/subuser_access", r.client.BaseURL)
		reqSA.Method = "GET"
		// page size hint (SendGrid may ignore, but set for clarity)
		if reqSA.QueryParams == nil {
			reqSA.QueryParams = make(map[string]string)
		}
		reqSA.QueryParams["limit"] = "100"
		if afterID > 0 {
			reqSA.QueryParams["after_subuser_id"] = strconv.FormatInt(afterID, 10)
		}

		saResp, err := sendgrid.API(reqSA)
		if err != nil {
			resp.Diagnostics.AddError("SendGrid API error (subuser_access)", err.Error())
			return
		}
		if saResp.StatusCode >= 300 {
			resp.Diagnostics.AddError("Read subuser access failed", fmt.Sprintf("status=%d body=%s", saResp.StatusCode, saResp.Body))
			return
		}
		var sa teammateSubuserAccessResponse
		if err := json.Unmarshal([]byte(saResp.Body), &sa); err != nil {
			resp.Diagnostics.AddError("Parse error (subuser_access)", fmt.Sprintf("unable to parse body: %v", err))
			return
		}
		hasRestricted = sa.HasRestrictedSubuserAccess
		for _, e := range sa.SubuserAccess {
			allEntries = append(allEntries, subuserAccessEntry{
				ID:             e.ID,
				PermissionType: e.PermissionType,
				Scopes:         e.Scopes,
			})
		}
		// pagination
		if sa.Metadata.NextParams.AfterSubuserID == 0 {
			break
		}
		afterID = sa.Metadata.NextParams.AfterSubuserID
	}

	// normalize identifiers from API response
	state.Email = types.StringValue(got.Email)
	state.ID = types.StringValue(got.Email)
	// propagate names from API into state for import parity
	if got.FirstName != "" {
		state.FirstName = types.StringValue(got.FirstName)
	} else {
		state.FirstName = types.StringNull()
	}
	if got.LastName != "" {
		state.LastName = types.StringValue(got.LastName)
	} else {
		state.LastName = types.StringNull()
	}
	// Map back to state fields
	state.HasRestricted = types.BoolValue(hasRestricted)
	// Convert entries to the ListNestedBlock model `[]subuserAccessObject`
	if len(allEntries) > 0 {
		objs := make([]subuserAccessObject, 0, len(allEntries))
		for _, e := range allEntries {
			o := subuserAccessObject{
				ID:             types.Int64Value(e.ID),
				PermissionType: types.StringValue(e.PermissionType),
			}
			// scopes -> types.Set
			if len(e.Scopes) > 0 {
				setVals := make([]attr.Value, 0, len(e.Scopes))
				for _, s := range e.Scopes {
					setVals = append(setVals, types.StringValue(s))
				}
				o.Scopes, _ = types.SetValue(types.StringType, setVals)
			} else {
				o.Scopes = types.SetNull(types.StringType)
			}
			objs = append(objs, o)
		}
		// assign to state.SubuserAccess
		lv, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: map[string]attr.Type{
			"id": types.Int64Type, "permission_type": types.StringType, "scopes": types.SetType{ElemType: types.StringType},
		}}, objs)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.SubuserAccess = lv
	} else {
		state.SubuserAccess = types.ListNull(types.ObjectType{AttrTypes: map[string]attr.Type{
			"id": types.Int64Type, "permission_type": types.StringType, "scopes": types.SetType{ElemType: types.StringType},
		}})
	}

	state.Status = types.StringValue(got.Status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SSOTeammateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Not configured", "Provider configuration is missing")
		return
	}

	var plan ssoTeammateModel
	var state ssoTeammateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	username := state.Email.ValueString() // email を username として扱う

	patch := ssoPatchPayload{}
	if !plan.FirstName.IsNull() && !plan.FirstName.IsUnknown() {
		v := plan.FirstName.ValueString()
		patch.FirstName = &v
	}
	if !plan.LastName.IsNull() && !plan.LastName.IsUnknown() {
		v := plan.LastName.ValueString()
		patch.LastName = &v
	}
	if !plan.HasRestricted.IsNull() && !plan.HasRestricted.IsUnknown() {
		v := plan.HasRestricted.ValueBool()
		patch.HasRestricted = &v
	}
	// subuser_access
	if !plan.SubuserAccess.IsNull() && !plan.SubuserAccess.IsUnknown() {
		var objs []subuserAccessObject
		resp.Diagnostics.Append(plan.SubuserAccess.ElementsAs(ctx, &objs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		for _, o := range objs {
			entry := subuserAccessEntry{ID: o.ID.ValueInt64(), PermissionType: o.PermissionType.ValueString()}
			if !o.Scopes.IsNull() && !o.Scopes.IsUnknown() {
				var scopes []string
				resp.Diagnostics.Append(o.Scopes.ElementsAs(ctx, &scopes, false)...)
				if resp.Diagnostics.HasError() {
					return
				}
				entry.Scopes = scopes
			}
			patch.SubuserAccess = append(patch.SubuserAccess, entry)
		}
	}

	b, _ := json.Marshal(patch)
	reqSG := sendgrid.GetRequest(r.client.APIKey, "/v3/sso/teammates/"+username, r.client.BaseURL)
	reqSG.Method = "PATCH"
	reqSG.Body = b
	sgResp, err := sendgrid.API(reqSG)
	if err != nil {
		resp.Diagnostics.AddError("SendGrid API error", err.Error())
		return
	}
	if sgResp.StatusCode >= 300 {
		resp.Diagnostics.AddError("Update SSO Teammate failed",
			fmt.Sprintf("status=%d body=%s", sgResp.StatusCode, sgResp.Body))
		return
	}

	// ---- Post-update readback to ensure all Computed attrs are known ----
	tflog.Debug(ctx, "Post-update GET /v3/teammates/{username}", map[string]any{"username": username})
	reqGet := sendgrid.GetRequest(r.client.APIKey, "/v3/teammates/"+username, r.client.BaseURL)
	reqGet.Method = "GET"
	getResp, err := sendgrid.API(reqGet)
	if err != nil {
		resp.Diagnostics.AddError("SendGrid API error (post-update read)", err.Error())
		return
	}
	if getResp.StatusCode >= 300 {
		resp.Diagnostics.AddError("Post-update read failed", fmt.Sprintf("status=%d body=%s", getResp.StatusCode, getResp.Body))
		return
	}
	var got teammateGetResponse
	if err := json.Unmarshal([]byte(getResp.Body), &got); err != nil {
		resp.Diagnostics.AddError("Parse error (post-update teammate)", fmt.Sprintf("unable to parse body: %v", err))
		return
	}

	var allEntries []subuserAccessEntry
	var hasRestricted bool
	var afterID int64 = 0
	for {
		tflog.Debug(ctx, "Post-update GET /v3/teammates/{username}/subuser_access", map[string]any{"username": username, "after_subuser_id": afterID})
		reqSA := sendgrid.GetRequest(r.client.APIKey, "/v3/teammates/"+username+"/subuser_access", r.client.BaseURL)
		reqSA.Method = "GET"
		if reqSA.QueryParams == nil {
			reqSA.QueryParams = make(map[string]string)
		}
		reqSA.QueryParams["limit"] = "100"
		if afterID > 0 {
			reqSA.QueryParams["after_subuser_id"] = strconv.FormatInt(afterID, 10)
		}

		saResp, err := sendgrid.API(reqSA)
		if err != nil {
			resp.Diagnostics.AddError("SendGrid API error (post-update subuser_access)", err.Error())
			return
		}
		if saResp.StatusCode >= 300 {
			resp.Diagnostics.AddError("Post-update subuser_access read failed", fmt.Sprintf("status=%d body=%s", saResp.StatusCode, saResp.Body))
			return
		}
		var sa teammateSubuserAccessResponse
		if err := json.Unmarshal([]byte(saResp.Body), &sa); err != nil {
			resp.Diagnostics.AddError("Parse error (post-update subuser_access)", fmt.Sprintf("unable to parse body: %v", err))
			return
		}
		hasRestricted = sa.HasRestrictedSubuserAccess
		for _, e := range sa.SubuserAccess {
			allEntries = append(allEntries, subuserAccessEntry{ID: e.ID, PermissionType: e.PermissionType, Scopes: e.Scopes})
		}
		if sa.Metadata.NextParams.AfterSubuserID == 0 {
			break
		}
		afterID = sa.Metadata.NextParams.AfterSubuserID
	}

	if got.FirstName != "" {
		plan.FirstName = types.StringValue(got.FirstName)
	} else {
		plan.FirstName = types.StringNull()
	}
	if got.LastName != "" {
		plan.LastName = types.StringValue(got.LastName)
	} else {
		plan.LastName = types.StringNull()
	}
	plan.Status = types.StringValue(got.Status)
	plan.HasRestricted = types.BoolValue(hasRestricted)

	if len(allEntries) > 0 {
		objs := make([]subuserAccessObject, 0, len(allEntries))
		for _, e := range allEntries {
			o := subuserAccessObject{ID: types.Int64Value(e.ID), PermissionType: types.StringValue(e.PermissionType)}
			if len(e.Scopes) > 0 {
				setVals := make([]attr.Value, 0, len(e.Scopes))
				for _, s := range e.Scopes {
					setVals = append(setVals, types.StringValue(s))
				}
				o.Scopes, _ = types.SetValue(types.StringType, setVals)
			} else {
				o.Scopes = types.SetNull(types.StringType)
			}
			objs = append(objs, o)
		}
		lv, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: map[string]attr.Type{
			"id": types.Int64Type, "permission_type": types.StringType, "scopes": types.SetType{ElemType: types.StringType},
		}}, objs)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.SubuserAccess = lv
	} else {
		plan.SubuserAccess = types.ListNull(types.ObjectType{AttrTypes: map[string]attr.Type{
			"id": types.Int64Type, "permission_type": types.StringType, "scopes": types.SetType{ElemType: types.StringType},
		}})
	}
	plan.ID = types.StringValue(plan.Email.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SSOTeammateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Not configured", "Provider configuration is missing")
		return
	}
	var state ssoTeammateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	username := state.Email.ValueString()
	reqSG := sendgrid.GetRequest(r.client.APIKey, "/v3/teammates/"+username, r.client.BaseURL)
	reqSG.Method = "DELETE"
	sgResp, err := sendgrid.API(reqSG)
	if err != nil {
		resp.Diagnostics.AddError("SendGrid API error", err.Error())
		return
	}
	if sgResp.StatusCode >= 300 && sgResp.StatusCode != 404 {
		resp.Diagnostics.AddError("Delete teammate failed",
			fmt.Sprintf("status=%d body=%s", sgResp.StatusCode, sgResp.Body))
		return
	}
}

// ImportState allows `terraform import sendgrid_sso_teammate.example <email>`.
func (r *SSOTeammateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
