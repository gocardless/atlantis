package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {

	conf, err := parseConfig()
	defer conf.clean()
	if err != nil {
		fmt.Println(err)
		flag.Usage()
		return
	}

	targetProjID := conf.svcPlan.getProjectID()

	fmt.Println("\nInitializing import generator and parsing service plan...")
	importGen := newImportGenerator(*conf.svcTfConf, conf.svcPlan)

	if len(importGen.projectResources) > 0 && !conf.skipRegistry {
		fmt.Println("\nGenerating GCP project related stuff...")
		if !conf.skipInit {
			err = conf.regTfConf.initTerraform()
			if err != nil {
				fmt.Println(err)
				return
			}
		}

		err = conf.generateRegistryState()
		if err != nil {
			fmt.Println(err)
			flag.Usage()
			return
		}

		regManager := newLegacyTfManager(*conf.regTfConf, targetProjID, conf.regState)
		importGen.setRegistryManager(&regManager)
	} else {
		fmt.Println("\nGCP Project related resources not needed. Continuing...")
	}

	if len(importGen.cnrmResources) > 0 && !conf.skipClusters {
		fmt.Println("Generating CNRM related stuff...")
		if !conf.skipInit {
			err = conf.clustTfConf.initTerraform()
			if err != nil {
				fmt.Println(err)
				return
			}
		}

		err = conf.generateClustersState()
		if err != nil {
			fmt.Println(err)
			return
		}

		clustManager := newLegacyTfManager(*conf.clustTfConf, targetProjID, conf.clustState)
		importGen.setClustersManager(&clustManager)
	} else {
		fmt.Println("\nCNRM related resources not needed. Continuing...")
	}

	fmt.Println("\nGenerating needed commands...")
	err = importGen.generateCmds(conf)
	if err != nil {
		fmt.Printf("\nError generating commands: %s", err)
		return
	}

	if len(importGen.projectResources) > 0 && !conf.skipRegistry {
		fmt.Println("GCP Project Imports:")
		importGen.renderImportCmds(importGen.projectImports)
		err = sanityCheckRes(importGen.projectImports, importGen.regManager)
		if err != nil {
			fmt.Printf("WARNING: following errors detected while doing sanity check. Inspect output and ensure you want to continue:\n%s\n", err)
		} else {
			fmt.Println("Sanity check successful.")
		}

		userResp, err := userConfirmation()
		if err != nil {
			fmt.Println(err)
			return
		}
		if userResp {
			importGen.doImports(importGen.projectImports)
		}
	} else {
		fmt.Println("No GCP Project imports, or imports skipped.")
	}

	if len(importGen.cnrmResources) > 0 && !conf.skipClusters {
		fmt.Println("CNRM Imports:")
		importGen.renderImportCmds(importGen.cnrmImports)
		err = sanityCheckRes(importGen.cnrmImports, importGen.clustersManager)
		if err != nil {
			fmt.Printf("WARNING: following errors detected while doing sanity check. Inspect output and ensure you want to continue:\n%s\n", err)
		} else {
			fmt.Println("Sanity check successful.")
		}

		userResp, err := userConfirmation()
		if err != nil {
			fmt.Println(err)
			return
		}
		if userResp {
			importGen.doImports(importGen.cnrmImports)
		}
	} else {
		fmt.Println("No CNRM imports, or imports skipped.")
	}

	fmt.Println("WARNING: After this point, state rm commands will be executed. Ensure you have atlantis locks!\nAnswering N or Q to following prompt will abort program.")
	userResp, err := userConfirmation()
	if err != nil || !userResp {
		fmt.Println(err)
		return
	}

	if len(importGen.projectResources) > 0 && !conf.skipRegistry {
		fmt.Println("GCP Project Deletions:")
		importGen.regManager.renderDeleteCmds()

		importGen.regManager.doDeletions(true)

		fmt.Println("WARNING: About to execute state rm commands! Ensure registry project has Atlantis lock")
		userResp, err = userConfirmation()
		if err != nil {
			fmt.Println(err)
			return
		}
		if userResp {
			importGen.regManager.doDeletions(false)
		}
	} else {
		fmt.Println("No GCP Project deletions or skipped..")
	}

	if len(importGen.cnrmResources) > 0 && !conf.skipClusters {
		fmt.Println("CNRM Deletions:")
		importGen.clustersManager.renderDeleteCmds()

		importGen.clustersManager.doDeletions(true)

		fmt.Println("WARNING: About to execute state rm commands! Ensure clusters project has Atlantis lock")
		userResp, err = userConfirmation()
		if err != nil {
			fmt.Println(err)
			return
		}
		if userResp {
			importGen.clustersManager.doDeletions(false)
		}
	} else {
		fmt.Println("No CNRM deletions or skipped..")
	}
	fmt.Println("End of run.")
}

func userConfirmation() (bool, error) {
	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Continue execution? [Y/n/q] : ")
		res, err := r.ReadString('\n')
		if err != nil {
			return false, err
		}

		res = strings.ToUpper(strings.TrimSpace(res))

		if res == "" || res == "Y" {
			return true, nil
		} else if res == "N" {
			return false, nil
		} else if res == "Q" {
			return false, fmt.Errorf("user aborted")
		} else {
			fmt.Println("Invalid response!")
		}
	}
}

type legacyTfManager struct {
	tfConfig  tfParams
	resources []TfResource
	deletions []string
}

func newLegacyTfManager(tfConfig tfParams, project string, state *TfState) legacyTfManager {
	ret := legacyTfManager{
		tfConfig:  tfConfig,
		resources: []TfResource{},
		deletions: []string{},
	}

	// HACK: registry project only has root module, while clusters project
	// has child modules.
	if len(state.Values.RootModule.ChildModules) == 0 {
		ret.resources = getRegProjResources(project, state)
	} else {
		ret.resources = getClustersProjResources(project, state)
	}

	return ret
}

func (tfMan *legacyTfManager) addDeletion(address string) {
	tfMan.deletions = append(tfMan.deletions, address)
}

func (tfMan *legacyTfManager) renderDeleteCmds() {
	for _, addr := range tfMan.deletions {
		fmt.Printf("%s state rm '%s'\n", tfMan.tfConfig.tfCmd, addr)
	}
}

func (tfMan *legacyTfManager) doDeletions(dryRun bool) {
	var err error

	// do a dry run first
	cmd := makeBaseTfCommand(&tfMan.tfConfig)
	cmd.Args = append(cmd.Args, "state", "rm")
	if dryRun {
		cmd.Args = append(cmd.Args, "-dry-run")
	}
	cmd.Args = append(cmd.Args, tfMan.deletions...)
	err = runCmdWrapper(cmd)
	if err != nil {
		fmt.Printf("Error deleting from state (dry-run) : %s\n", err)
	}

	if dryRun {
		fmt.Println("State deletion dry-run finished.")
	} else {
		fmt.Println("State deletion execution finished.")
	}
}

func (tfMan *legacyTfManager) getLegacyIdAddr(filter func(tr TfResource) bool) (string, string, error) {
	serviceAccounts := resFilterFunc(tfMan.resources, filter)
	if len(serviceAccounts) != 1 {
		return "", "", fmt.Errorf("Expected 1 filtered resource, found: %v", len(serviceAccounts))
	}

	return serviceAccounts[0].Values.ID, serviceAccounts[0].Address, nil
}

type importGenerator struct {
	tfConfig tfParams

	// resources from plan to be imported
	projectResources []TfResource
	cnrmResources    map[string][]TfResource

	// resources to import - "resource_address: identifier_arguments"
	projectImports map[string]string
	cnrmImports    map[string]string

	regManager      *legacyTfManager
	clustersManager *legacyTfManager

	projectID string
}

func countRes(list []TfResource) int {
	return len(resFilterFunc(list, func(tr TfResource) bool {
		return tr.Mode != "data"
	}))
}

func newImportGenerator(svcTfConfig tfParams, plan *TfPlan) importGenerator {
	ret := importGenerator{
		tfConfig:        svcTfConfig,
		regManager:      nil,
		clustersManager: nil,
	}

	ret.projectResources, ret.projectID = getPlannedProjResources(plan)
	fmt.Printf("Ingested %d planned project resources.\n", countRes(ret.projectResources))
	ret.cnrmResources = getPlannedCnrmResources(plan)
	totalCnrmRes := 0
	for _, list := range ret.cnrmResources {
		totalCnrmRes += countRes(list)
	}
	fmt.Printf("Ingested %d planned cnrm resources in %d namespaces.\n", totalCnrmRes, len(ret.cnrmResources))

	return ret
}

// generateCmds generates import/deletion commands for modules which are present in the plan
func (ig *importGenerator) generateCmds(conf *config) error {
	if len(ig.projectResources) > 0 && !conf.skipRegistry {
		if ig.regManager == nil {
			return fmt.Errorf("generate project commands: no registry manager defined. Command count: %d", len(ig.projectResources))
		}
		err := ig.generateProjCmds()
		if err != nil {
			return fmt.Errorf("generate project commands: %w", err)
		}
	}

	if len(ig.cnrmResources) > 0 && !conf.skipClusters {
		if ig.clustersManager == nil {
			return fmt.Errorf("generate cnrm commands: no clusters manager defined. Command count: %d", len(ig.cnrmResources))
		}
		err := ig.generateClusterCmds()
		if err != nil {
			return fmt.Errorf("generate cnrm commands: %w", err)
		}
	}

	return nil
}

func (ig *importGenerator) setRegistryManager(rm *legacyTfManager) {
	ig.regManager = rm
}

func (ig *importGenerator) setClustersManager(cm *legacyTfManager) {
	ig.clustersManager = cm
}

func (ig *importGenerator) renderImportCmds(list map[string]string) {
	for addr, arg := range list {
		fmt.Printf("%s import -var-file %s '%s' '%s'\n", ig.tfConfig.tfCmd, ig.tfConfig.varsFile, addr, arg)
	}
}

func (ig *importGenerator) doImports(list map[string]string) {
	var err error
	for addr, arg := range list {
		cmd := makeBaseTfCommand(&ig.tfConfig)
		cmd.Args = append(cmd.Args, "import", "-compact-warnings", "-var-file", ig.tfConfig.varsFile, addr, arg)
		err = runCmdWrapper(cmd)
		if err != nil {
			fmt.Printf("Error importing: %s", err)
		}
	}
	fmt.Println("Import batch finished.")
}

func sanityCheckRes(imports map[string]string, legacyMan *legacyTfManager) error {
	fmt.Println("Preforming sanity check...")
	retStr := ""
	if len(imports) != len(legacyMan.deletions) {
		retStr = fmt.Sprintf("%sImport count %d is not equal to deletion count %d\n", retStr, len(imports), len(legacyMan.deletions))
	}

	newDelList := make([]string, len(legacyMan.deletions))
	for _, del := range legacyMan.deletions {
		filteredRes := resFilterFunc(legacyMan.resources, func(tr TfResource) bool {
			return tr.Address == del
		})
		if len(filteredRes) == 0 {
			retStr = fmt.Sprintf("%sDeletable resource not found in state (will not be deleted): %s\n", retStr, del)
			continue
		} else if len(filteredRes) > 1 {
			retStr = fmt.Sprintf("%sMultiple resources found with address (this should not happen): %s\n", retStr, del)
		}
		newDelList = append(newDelList, del)
	}

	if retStr != "" {
		legacyMan.deletions = newDelList
		return fmt.Errorf("%s", retStr)
	}

	return nil
}

// generateProjCmds populates the list of importable resources related to GCP project
// for each resource, also adds the corresponding resource to pending deletions.
func (ig *importGenerator) generateProjCmds() error {
	imports := make(map[string]string)
	for _, resource := range ig.projectResources {
		switch resource.Type {
		case "google_project":
			legacyProjAddr := fmt.Sprintf("google_project.project[\"%s\"]", ig.projectID)

			imports[resource.Address] = ig.projectID
			ig.regManager.addDeletion(legacyProjAddr)

		case "google_billing_budget":
			billingBudgetID, billingBudgetLegacyAddr, err := ig.regManager.getLegacyIdAddr(func(tr TfResource) bool {
				return tr.Type == "google_billing_budget"
			})
			if err != nil {
				return err
			}

			imports[resource.Address] = billingBudgetID
			ig.regManager.addDeletion(billingBudgetLegacyAddr)

		case "google_project_iam_member":
			iamResArg, legacyIamResAddr, err := ig.getIamMemberData(resource, ig.regManager)
			if err != nil {
				return err
			}

			imports[resource.Address] = iamResArg
			ig.regManager.addDeletion(legacyIamResAddr)

		case "google_project_service":
			srvResArg, legacySvcAddr := ig.getProjService(resource)

			imports[resource.Address] = srvResArg
			ig.regManager.addDeletion(legacySvcAddr)
		}
	}

	ig.projectImports = imports
	return nil
}

func (ig *importGenerator) generateClusterCmds() error {
	imports := make(map[string]string)

	for ns, resList := range ig.cnrmResources {
		// we need the host cluster project for this so we extract it
		svcAccounts := resFilterFunc(resList, func(tr TfResource) bool {
			return tr.Type == "google_service_account"
		})
		if len(svcAccounts) != 1 {
			return fmt.Errorf("Expected to find 1 service account in cnrm namespace %s, but found %v", ns, len(svcAccounts))
		}
		hostProject := svcAccounts[0].Values.Project

		for _, resource := range resList {
			switch resource.Type {

			case "google_service_account":
				svcAccID, legacySvcAccAddr, err := ig.clustersManager.getLegacyIdAddr(func(tr TfResource) bool {
					return tr.Values.Project == hostProject && tr.Index == ns
				})
				if err != nil {
					return err
				}

				imports[resource.Address] = svcAccID
				ig.clustersManager.addDeletion(legacySvcAccAddr)

			case "google_project_iam_member":
				iamResArg, legacyIamResAddr, err := ig.getCnrmIamMemberData(resource, hostProject, ns)
				if err != nil {
					return err
				}

				imports[resource.Address] = iamResArg
				ig.clustersManager.addDeletion(legacyIamResAddr)

			case "google_service_account_iam_policy":
				policyArg, legacyPolicyAddr, err := ig.clustersManager.getLegacyIdAddr(func(tr TfResource) bool {
					return tr.Type == "google_service_account_iam_policy" && tr.Index == ns && strings.Contains(tr.Values.ID, hostProject)
				})
				if err != nil {
					return err
				}

				imports[resource.Address] = policyArg
				ig.clustersManager.addDeletion(legacyPolicyAddr)
			}
		}
	}

	ig.cnrmImports = imports
	return nil
}

// resFilterFunc filters resource slice via filter function
func resFilterFunc(resSl []TfResource, ffunc func(TfResource) bool) []TfResource {
	ret := []TfResource{}
	for _, res := range resSl {
		if ffunc(res) {
			ret = append(ret, res)
		}
	}

	return ret
}

// getIamMemberData returns google_project_iam_member import argument
// string and address from old project
func (ig *importGenerator) getIamMemberData(resource TfResource, legacyMan *legacyTfManager) (string, string, error) {
	role := resource.Values.Role
	member := resource.Values.Member
	importArg := fmt.Sprintf("%s %s %s", ig.projectID, role, member)

	legacyRes := resFilterFunc(legacyMan.resources, func(tr TfResource) bool {
		return (tr.Values.Project == ig.projectID) && (tr.Values.Role == role) && (tr.Values.Member == member)
	})
	if len(legacyRes) != 1 {
		return "", "", fmt.Errorf("Expected 1 resource with member=%s; role=%s, but found: %v", member, role, len(legacyRes))
	}

	return importArg, legacyRes[0].Address, nil
}

// getCnrmIamMemberData returns google_project_iam_member import argument
// string and address from old project.
// We need a special function for CNRM because we don't know the member yet
func (ig *importGenerator) getCnrmIamMemberData(resource TfResource, hostProject string, namespace string) (string, string, error) {
	role := resource.Values.Role

	legacyRes := resFilterFunc(ig.clustersManager.resources, func(tr TfResource) bool {
		return (tr.Values.Project == ig.projectID) && (tr.Values.Role == role) && (tr.Index == namespace) && strings.Contains(tr.Values.ID, hostProject)
	})
	if len(legacyRes) != 1 {
		return "", "", fmt.Errorf("Expected 1 resource with role=%s, namespace=%s, hostProject=%s, but found: %v", role, namespace, hostProject, len(legacyRes))
	}
	importArg := fmt.Sprintf("%s %s %s", ig.projectID, role, legacyRes[0].Values.Member)

	return importArg, legacyRes[0].Address, nil
}

func (ig *importGenerator) getProjService(resource TfResource) (string, string) {
	importArg := fmt.Sprintf("%s/%s", ig.projectID, resource.Values.Service)
	legacyRes := fmt.Sprintf("google_project_service.service[\"%s.%s\"]", ig.projectID, resource.Values.Service)
	return importArg, legacyRes
}

func getRegProjResources(projectID string, regState *TfState) []TfResource {
	return resFilterFunc(regState.Values.RootModule.Resources, func(tr TfResource) bool {
		return strings.Contains(tr.Address, projectID)
	})
}

func getClustersProjResources(projectID string, state *TfState) []TfResource {
	var ret []TfResource
	for _, module := range state.Values.RootModule.ChildModules {
		// only gke_cluster_project modules contain the cnrm resources
		if strings.Contains(module.Address, "gke_cluster_project") {
			// filter resources of this projectID in this cluster_project
			// we extract only one resource from each namespace to get a list of namespaces
			resWithProj := resFilterFunc(module.Resources, func(tr TfResource) bool {
				return tr.Values.Project == projectID && tr.Type == "google_project_iam_member" && tr.Name == "cnrm_system"
			})

			// loop over namespaces - collect all resources with the same index as
			// the one we know for sure belongs to our project
			for _, res := range resWithProj {
				ret = append(ret, resFilterFunc(module.Resources, func(tr TfResource) bool {
					return tr.Index == res.Index
				})...)
			}
		}
	}

	return ret
}

func getPlannedProjResources(plan *TfPlan) ([]TfResource, string) {
	for _, module := range plan.PlannedValues.RootModule.ChildModules {
		if module.Address == "module.google_project[0]" {
			return module.Resources, plan.getProjectID()
		}
	}
	return nil, ""
}

func getPlannedCnrmResources(plan *TfPlan) map[string][]TfResource {
	ret := make(map[string][]TfResource)
	for _, module := range plan.PlannedValues.RootModule.ChildModules {
		if strings.Contains(module.Address, "module.cnrm_iam") {
			namespace := strings.Split(module.Address, "\"")[1]
			ret[namespace] = module.Resources
		}
	}
	return ret
}
