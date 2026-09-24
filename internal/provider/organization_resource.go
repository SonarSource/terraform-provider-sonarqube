package provider

import (
	"context"
	"errors"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
)

var (
	_ resource.Resource                = &organizationResource{}
	_ resource.ResourceWithConfigure   = &organizationResource{}
	_ resource.ResourceWithImportState = &organizationResource{}
)

// organizationKeyPattern is the rule the server applies: lower-case letters,
// digits and dashes, with no leading and no trailing dash.
var organizationKeyPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// NewOrganizationResource returns the sonarqube_organization resource.
func NewOrganizationResource() resource.Resource {
	return &organizationResource{}
}

type organizationResource struct {
	client *client.Client
}

type organizationResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Key         types.String `tfsdk:"key"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	URL         types.String `tfsdk:"url"`
	AvatarURL   types.String `tfsdk:"avatar_url"`
}

func (r *organizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *organizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a SonarQube Cloud organization. Organizations exist in " +
			"SonarQube Cloud only.\n\n" +
			"Take care against a production instance: a deletion starts billing events, " +
			"and it can leave a binding to a DevOps platform behind.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identifier of the organization. Holds the same value as `key`.",
				// The id follows the key, so it must be planned from the
				// planned key. Taken from the state instead, a rename would
				// plan the old key here and the apply would contradict the
				// plan.
				PlanModifiers: []planmodifier.String{keyPlanModifier{}},
			},
			"key": schema.StringAttribute{
				Required: true,
				Description: "Key of the organization. Must be unique. A new key renames the " +
					"organization in place and keeps its projects.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					stringvalidator.RegexMatches(organizationKeyPattern,
						"must hold lower-case letters, digits and dashes only, with no leading and no trailing dash"),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the organization.",
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Description of the organization.",
				Validators:  []validator.String{stringvalidator.LengthAtMost(256)},
			},
			"url": schema.StringAttribute{
				Optional:    true,
				Description: "Address of the web page of the organization.",
				Validators:  []validator.String{stringvalidator.LengthAtMost(256)},
			},
			"avatar_url": schema.StringAttribute{
				Optional:    true,
				Description: "Address of the avatar of the organization.",
				Validators:  []validator.String{stringvalidator.LengthAtMost(256)},
			},
		},
	}
}

func (r *organizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, diagnostics := requireCloudClient(req.ProviderData,
		"The sonarqube_organization resource manages an organization")
	resp.Diagnostics.Append(diagnostics...)
	r.client = c
}

func (r *organizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := plan.Key.ValueString()

	// Leave out every attribute the configuration does not set, so that the
	// server applies its own default.
	err := r.client.CreateOrganization(ctx, client.OrganizationRequest{
		Key:         key,
		Name:        valueIfSet(plan.Name),
		Description: valueIfSet(plan.Description),
		URL:         valueIfSet(plan.URL),
		Avatar:      valueIfSet(plan.AvatarURL),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the organization "+key, err.Error())
		return
	}

	// The organization exists from here on. The framework starts the state of
	// a create as null, so a read back that fails would leave an organization
	// that Terraform does not know, and the next apply would fail on a key
	// that is already taken.
	r.recordAndRead(ctx, key, plan, &resp.State, &resp.Diagnostics)
}

func (r *organizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := state.Key.ValueString()

	org, err := r.client.GetOrganization(ctx, key)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			// The API answers the same way for an organization that is gone
			// and for one that this token may not see, and there is no third
			// answer to tell them apart. Treat it as gone, which is what every
			// provider does here: the next plan then makes the organization
			// again. A token that lost its permission gives a create that
			// fails on a key that exists, which is loud enough to diagnose.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Cannot read the organization "+key, err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, stateFromOrganization(org, state))...)
}

func (r *organizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state organizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The framework starts the state of an update as the prior state, not as
	// the plan: server_updateresource.go sets updateResp.State from
	// req.PriorState. A failure below therefore keeps what the server really
	// holds, and needs nothing written here.
	key := plan.Key.ValueString()

	if priorKey := state.Key.ValueString(); key != priorKey {
		// A new key is a rename, and it goes first, because every other call
		// finds the organization by its key.
		if err := r.client.UpdateOrganizationKey(ctx, priorKey, key); err != nil {
			resp.Diagnostics.AddError("Cannot rename the organization "+priorKey+" to "+key, err.Error())
			return
		}

		// Record the rename now. A failure below must not leave the state
		// pointing at a key that no longer exists.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), key)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), key)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// The server keeps the current value of a parameter that is absent, so
	// every field goes with the request: an attribute taken out of the
	// configuration must arrive as empty to clear it.
	err := r.client.UpdateOrganization(ctx, client.OrganizationRequest{
		Key:         key,
		Name:        valueOrEmpty(plan.Name),
		Description: valueOrEmpty(plan.Description),
		URL:         valueOrEmpty(plan.URL),
		Avatar:      valueOrEmpty(plan.AvatarURL),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the organization "+key, err.Error())
		return
	}

	// The server holds the planned values now, so record them before the read
	// back. A read that fails would otherwise leave the state describing an
	// organization that no longer looks like that.
	r.recordAndRead(ctx, key, plan, &resp.State, &resp.Diagnostics)
}

func (r *organizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := state.Key.ValueString()

	// The client answers a organization that is already gone with success.
	if err := r.client.DeleteOrganization(ctx, key); err != nil {
		resp.Diagnostics.AddError("Cannot delete the organization "+key, err.Error())
	}
}

func (r *organizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("key"), req, resp)
}
