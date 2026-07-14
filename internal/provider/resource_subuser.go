package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/sendgrid/sendgrid-go"
)

// NOTE: This resource manages Subusers via the SendGrid Subusers API.
//
// API Endpoints:
//   - Create: POST   /v3/subusers
//   - Read:   GET    /v3/subusers?username={username}  (no single-item GET endpoint exists)
//   - Update: PATCH  /v3/subusers/{username}           (toggle `disabled` only)
//   - Delete: DELETE /v3/subusers/{username}
//
// API Documentation:
//   - Create Subuser: https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/create-subuser
//   - List Subusers:  https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/list-all-subusers
//   - Update Subuser: https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/enable-disable-subuser
//   - Delete Subuser: https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/delete-subuser
//
// Scope: This resource manages subuser creation, its enabled/disabled state,
// and deletion. Ongoing IP assignment (PUT /v3/subusers/{username}/ips) is out
// of scope and intended to be handled by a separate resource; changing `ips`
// here forces replacement.

var _ resource.Resource = (*SubuserResource)(nil)
var _ resource.ResourceWithConfigure = (*SubuserResource)(nil)
var _ resource.ResourceWithImportState = (*SubuserResource)(nil)

func NewSubuserResource() resource.Resource { return &SubuserResource{} }

type SubuserResource struct{ client *Client }

type subuserModel struct {
	ID       types.String `tfsdk:"id"`
	Username types.String `tfsdk:"username"`
	Email    types.String `tfsdk:"email"`
	Password types.String `tfsdk:"password"`
	IPs      types.Set    `tfsdk:"ips"`
	Disabled types.Bool   `tfsdk:"disabled"`
	Region   types.String `tfsdk:"region"`
}

func (r *SubuserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_subuser"
}

func (r *SubuserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SubuserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a Twilio SendGrid Subuser via `/v3/subusers`. Creation assigns an initial set of IPs; ongoing IP management is handled by a separate resource, so changing `ips` forces replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Subuser ID returned by the API, stored as a string.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"username": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Subuser username. Cannot be changed after creation.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"email": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Subuser email address. Cannot be changed after creation.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(3),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"password": schema.StringAttribute{
				Required:            true,
				Sensitive:           true,
				MarkdownDescription: "Subuser password. Used only at creation time; the API never returns it, so drift on this value cannot be detected. Changing it forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"ips": schema.SetAttribute{
				ElementType:         types.StringType,
				Required:            true,
				MarkdownDescription: "IP addresses assigned to the subuser at creation. Ongoing IP management is out of scope for this resource; changing this forces replacement.",
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.RequiresReplace(),
				},
			},
			"disabled": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether the subuser is disabled. Can be toggled after creation via PATCH.",
			},
			"region": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Region string returned by the API (may be empty).",
			},
		},
	}
}

// ---------- API payloads ----------

type subuserCreatePayload struct {
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Password string   `json:"password"`
	IPs      []string `json:"ips"`
}

// subuserCreateResponse is the body returned by POST /v3/subusers.
type subuserCreateResponse struct {
	ID       int64    `json:"user_id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	IPs      []string `json:"ips"`
}

type subuserPatchPayload struct {
	Disabled bool `json:"disabled"`
}

// ---------- CRUD ----------

// Create creates a Subuser.
// POST /v3/subusers
// https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/create-subuser
func (r *SubuserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Not configured", "Provider configuration is missing")
		return
	}

	var plan subuserModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var ips []string
	resp.Diagnostics.Append(plan.IPs.ElementsAs(ctx, &ips, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := subuserCreatePayload{
		Username: plan.Username.ValueString(),
		Email:    plan.Email.ValueString(),
		Password: plan.Password.ValueString(),
		IPs:      ips,
	}

	b, _ := json.Marshal(payload)
	reqSG := sendgrid.GetRequest(r.client.APIKey, "/v3/subusers", r.client.BaseURL)
	reqSG.Method = "POST"
	reqSG.Body = b

	sgResp, err := sendgrid.API(reqSG)
	if err != nil {
		resp.Diagnostics.AddError("SendGrid API error", err.Error())
		return
	}
	if sgResp.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			fmt.Sprintf("Create Subuser failed: %s", apiErrorMessage(sgResp.Body)),
			fmt.Sprintf("status=%d body=%s", sgResp.StatusCode, sgResp.Body))
		return
	}

	var created subuserCreateResponse
	if err := json.Unmarshal([]byte(sgResp.Body), &created); err != nil {
		resp.Diagnostics.AddError("Parse error (create subuser)", fmt.Sprintf("unable to parse body: %v", err))
		return
	}

	plan.ID = types.StringValue(strconv.FormatInt(created.ID, 10))

	// Read back to populate computed attributes (disabled, region).
	username := plan.Username.ValueString()
	got, found, diags := r.readSubuser(ctx, username)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError("Post-create read failed",
			fmt.Sprintf("subuser %q was not found immediately after creation", username))
		return
	}

	plan.ID = types.StringValue(strconv.FormatInt(got.ID, 10))
	plan.Email = types.StringValue(got.Email)
	plan.Disabled = types.BoolValue(got.Disabled)
	plan.Region = regionToStringValue(got.Region)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read fetches the current state of a Subuser.
// GET /v3/subusers?username={username}
// https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/list-all-subusers
func (r *SubuserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Not configured", "Provider configuration is missing")
		return
	}
	var state subuserModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	username := state.Username.ValueString()
	if username == "" {
		resp.Diagnostics.AddError("Missing identifier", "username is empty; cannot read resource")
		return
	}

	got, found, diags := r.readSubuser(ctx, username)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(strconv.FormatInt(got.ID, 10))
	state.Username = types.StringValue(got.Username)
	state.Email = types.StringValue(got.Email)
	state.Disabled = types.BoolValue(got.Disabled)
	state.Region = regionToStringValue(got.Region)
	// password は API から返らないため state の値をそのまま保持する。
	// ips も作成時のみの管理なので state を保持する。

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update toggles the disabled state of a Subuser.
// PATCH /v3/subusers/{username}
// https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/enable-disable-subuser
func (r *SubuserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Not configured", "Provider configuration is missing")
		return
	}

	var plan subuserModel
	var state subuserModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	username := state.Username.ValueString()

	// disabled 以外の変更可能属性は RequiresReplace 指定のため、ここでは disabled のみ扱う。
	if !plan.Disabled.Equal(state.Disabled) {
		payload := subuserPatchPayload{Disabled: plan.Disabled.ValueBool()}
		b, _ := json.Marshal(payload)
		reqSG := sendgrid.GetRequest(r.client.APIKey, "/v3/subusers/"+username, r.client.BaseURL)
		reqSG.Method = "PATCH"
		reqSG.Body = b
		sgResp, err := sendgrid.API(reqSG)
		if err != nil {
			resp.Diagnostics.AddError("SendGrid API error", err.Error())
			return
		}
		if sgResp.StatusCode >= 300 {
			resp.Diagnostics.AddError("Update Subuser failed",
				fmt.Sprintf("status=%d body=%s", sgResp.StatusCode, sgResp.Body))
			return
		}
	}

	// Read back to ensure computed attributes reflect the current remote state.
	got, found, diags := r.readSubuser(ctx, username)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError("Post-update read failed",
			fmt.Sprintf("subuser %q was not found after update", username))
		return
	}

	plan.ID = types.StringValue(strconv.FormatInt(got.ID, 10))
	plan.Email = types.StringValue(got.Email)
	plan.Disabled = types.BoolValue(got.Disabled)
	plan.Region = regionToStringValue(got.Region)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes a Subuser.
// DELETE /v3/subusers/{username}
// https://www.twilio.com/docs/sendgrid/api-reference/subusers-api/delete-subuser
func (r *SubuserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Not configured", "Provider configuration is missing")
		return
	}
	var state subuserModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	username := state.Username.ValueString()
	reqSG := sendgrid.GetRequest(r.client.APIKey, "/v3/subusers/"+username, r.client.BaseURL)
	reqSG.Method = "DELETE"
	sgResp, err := sendgrid.API(reqSG)
	if err != nil {
		resp.Diagnostics.AddError("SendGrid API error", err.Error())
		return
	}
	if sgResp.StatusCode >= 300 && sgResp.StatusCode != 404 {
		resp.Diagnostics.AddError("Delete Subuser failed",
			fmt.Sprintf("status=%d body=%s", sgResp.StatusCode, sgResp.Body))
		return
	}
}

// ImportState allows `terraform import sendgrid_subuser.example <username>`.
func (r *SubuserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("username"), req, resp)
}

// readSubuser looks up a single subuser by exact username via the list endpoint.
// Returns (item, found, diags). found=false means the subuser no longer exists.
func (r *SubuserResource) readSubuser(ctx context.Context, username string) (subuserAPI, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	reqSG := sendgrid.GetRequest(r.client.APIKey, "/v3/subusers", r.client.BaseURL)
	reqSG.Method = "GET"
	if reqSG.QueryParams == nil {
		reqSG.QueryParams = make(map[string]string)
	}
	reqSG.QueryParams["username"] = username
	reqSG.QueryParams["include_region"] = "true"

	tflog.Debug(ctx, "GET /v3/subusers", map[string]any{"username": username})

	sgResp, err := sendgrid.API(reqSG)
	if err != nil {
		diags.AddError("SendGrid API error", err.Error())
		return subuserAPI{}, false, diags
	}
	if sgResp.StatusCode >= 300 {
		diags.AddError("Read Subuser failed",
			fmt.Sprintf("status=%d body=%s", sgResp.StatusCode, sgResp.Body))
		return subuserAPI{}, false, diags
	}

	var items []subuserAPI
	if err := json.Unmarshal([]byte(sgResp.Body), &items); err != nil {
		diags.AddError("Parse error (read subuser)", fmt.Sprintf("unable to parse body: %v", err))
		return subuserAPI{}, false, diags
	}

	// The list endpoint filters by username but does a prefix/substring match on
	// some accounts; require an exact match to be safe.
	for _, it := range items {
		if it.Username == username {
			return it, true, diags
		}
	}
	return subuserAPI{}, false, diags
}

// regionToStringValue maps an API region string to a Terraform value,
// returning null when empty.
func regionToStringValue(region string) types.String {
	if region == "" {
		return types.StringNull()
	}
	return types.StringValue(region)
}

// apiErrorMessage extracts the first human-readable message from a SendGrid
// error response body of the form {"errors":[{"message":"...","field":"..."}]}.
// It falls back to the raw body when the shape is unexpected, so the caller can
// always surface something useful in the diagnostic summary.
func apiErrorMessage(body string) string {
	var parsed struct {
		Errors []struct {
			Message string `json:"message"`
			Field   string `json:"field"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil || len(parsed.Errors) == 0 {
		return body
	}
	return parsed.Errors[0].Message
}
