package main

import "strings"

type TfPlan struct {
	PlannedValues struct {
		RootModule TfModule `json:"root_module,omitempty"`
	} `json:"planned_values,omitempty"`
}

func (plan *TfPlan) getProjectID() string {
	for _, module := range plan.PlannedValues.RootModule.ChildModules {
		// from project module, we can extract the project ID from any resource
		if module.Address == "module.google_project[0]" {
			for _, resource := range module.Resources {
				if resource.Values.ProjectID != "" {
					return resource.Values.ProjectID
				}
			}

			// from cnrm_iam module, we can extract the target project ID only from one resource
		} else if strings.Contains(module.Address, "cnrm_iam") {
			projIamMembers := resFilterFunc(module.Resources, func(tr TfResource) bool {
				return tr.Type == "google_project_iam_member"
			})
			if len(projIamMembers) > 0 {
				return projIamMembers[0].Values.Project
			}
		}
	}

	return ""
}

type TfState struct {
	Values struct {
		RootModule TfModule `json:"root_module,omitempty"`
	} `json:"values,omitempty"`
}

type TfModule struct {
	Address      string       `json:"address,omitempty"`
	ChildModules []TfModule   `json:"child_modules,omitempty"`
	Resources    []TfResource `json:"resources,omitempty"`
}

type TfResource struct {
	Address string `json:"address,omitempty"`
	Type    string `json:"type,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Name    string `json:"name,omitempty"`
	Index   string `json:"index,omitempty"`
	Values  struct {
		AccountID      string `json:"account_id,omitempty"`
		ProjectID      string `json:"project_id,omitempty"`
		BillingAccount string `json:"billing_account,omitempty"`
		ID             string `json:"id,omitempty"`
		Member         string `json:"member,omitempty"`
		Project        string `json:"project,omitempty"`
		Role           string `json:"role,omitempty"`
		Service        string `json:"service,omitempty"`
		Binding        []struct {
			Condition []any    `json:"condition,omitempty"`
			Members   []string `json:"members,omitempty"`
			Role      string   `json:"role,omitempty"`
		} `json:"binding,omitempty"`
	} `json:"values,omitempty"`
}
