package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	DEFAULT_TARGET_PLAN_FILENAME   = "target-tfplan"
	DEFAULT_TARGET_JSON_FIlENAME   = "target-plan.json"
	DEFAULT_REGISTRY_JSON_FILENAME = "registry-state.json"
	DEFAULT_CLUSTERS_JSON_FILENAME = "clusters-state.json"
)

// tfParams holds configuration for a Terraform project
type tfParams struct {
	tfCmd     string
	varsFile  string
	path      string
	workspace string
}

// initTerraform does Terraform initialization
func (tf *tfParams) initTerraform() error {
	cmd := makeBaseTfCommand(tf)
	cmd.Args = append(cmd.Args, "init")
	err := runCmdWrapper(cmd)
	if err != nil {
		return fmt.Errorf("init terraform: %w", err)
	}

	return nil
}

type config struct {
	targetJsonFile   string
	registryJsonFile string
	clustersJsonFile string

	svcPlan    *TfPlan
	regState   *TfState
	clustState *TfState

	svcTfConf   *tfParams
	regTfConf   *tfParams
	clustTfConf *tfParams

	tempDir  string
	keepDir  bool
	skipInit bool

	skipRegistry bool
	skipClusters bool
}

func parseConfig() (*config, error) {

	var (
		legacyTfVersion string
		targetTfVersion string
		targetDir       string
		registryPath    string
		clustersPath    string
		targetVarsFile  string
		regVarsFile     string
		clustVarsFile   string

		err error
	)

	c := new(config)

	flag.StringVar(&targetDir, "target-dir", "", "Target Terraform project directory (defaults to CWD)")
	flag.StringVar(&registryPath, "registry-dir", "/anu/terraform/google/projects/registry", "Registry Terraform project directory")
	flag.StringVar(&clustersPath, "clusters-dir", "/anu/terraform/google/projects/clusters", "Clusters Terraform project directory")
	flag.StringVar(&c.targetJsonFile, "target-json", "", "(optional) target TF plan json file")
	flag.StringVar(&c.registryJsonFile, "registry-json", "", "(optional) registry TF state json file")
	flag.StringVar(&c.clustersJsonFile, "clusters-json", "", "(optional) clusters TF state json file")
	flag.StringVar(&legacyTfVersion, "legacy-tf-version", "0.13.7", "Terraform version for legacy projects")
	flag.StringVar(&targetTfVersion, "target-tf-version", "1.5.2", "Terraform version for target service project")
	flag.StringVar(&targetVarsFile, "target-vars-file", "", "Target service project tfvars relative path (default generated from workspace)")
	flag.StringVar(&regVarsFile, "registry-vars-file", "", "Registry project tfvars relative path (default generated from workspace)")
	flag.StringVar(&clustVarsFile, "clusters-vars-file", "", "Clusters project tfvars relative path (default generated from workspace)")
	flag.BoolVar(&c.keepDir, "keep-dir", false, "Keep temporary directory created")
	flag.BoolVar(&c.skipInit, "skip-init", false, "Skip Terraform initializations (must be initialized before)")
	flag.BoolVar(&c.skipRegistry, "skip-registry", false, "Skip any changes related to legacy Registry project")
	flag.BoolVar(&c.skipClusters, "skip-clusters", false, "Skip any changes related to legacy Clusters project")
	flag.Parse()

	if len(flag.Args()) != 2 {
		return nil, fmt.Errorf("Requires 2 positional arguments:\ncmd [options] <legacy-workspace> <target-workspace>")
	}

	legacyWorkspace := flag.Arg(0)
	targetWorkspace := flag.Arg(1)

	c.svcTfConf, err = generateSvcTfParams(targetDir, targetTfVersion, targetWorkspace, targetVarsFile)
	if err != nil {
		return nil, fmt.Errorf("generate service TF parameters: %w", err)
	}

	c.regTfConf, err = generateLegacyTfParams(registryPath, legacyTfVersion, legacyWorkspace, regVarsFile)
	if err != nil {
		return nil, fmt.Errorf("generate registry TF parameters: %w", err)
	}

	c.clustTfConf, err = generateLegacyTfParams(clustersPath, legacyTfVersion, legacyWorkspace, clustVarsFile)
	if err != nil {
		return nil, fmt.Errorf("generate clusters TF parameters: %w", err)
	}

	// service state is required, so generate when parsing config
	if !c.skipInit {
		err = c.svcTfConf.initTerraform()
		if err != nil {
			fmt.Println(err)
			return nil, err
		}
	}
	err = c.generateSvcPlan()
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return c, nil
}

func (c *config) clean() {
	if c.tempDir == "" {
		return
	}

	if c.keepDir {
		fmt.Printf("Keeping temporary directory: %s\n", c.tempDir)
		return
	}

	err := os.RemoveAll(c.tempDir)
	if err != nil {
		fmt.Printf("Error removing temp directory %s: %s", c.tempDir, err)
	}
}

func generateSvcTfParams(path, version, workspace, varsPath string) (*tfParams, error) {
	ret := &tfParams{
		tfCmd:     "terraform" + version,
		workspace: workspace,
	}

	if path != "" {
		ret.path = path
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("generate project path: %w", err)
		}
		ret.path = cwd
		fmt.Printf("No service directory provided, using current directory: %s\n", ret.path)
	}

	if varsPath != "" {
		ret.varsFile = varsPath
	} else {
		ret.varsFile = fmt.Sprintf("../environments/%s.tfvars", workspace)
		fmt.Printf("No vars file provided for service, using generated value: %s\n", ret.varsFile)
	}

	return ret, nil
}

func generateLegacyTfParams(path, version, workspace, varsPath string) (*tfParams, error) {
	ret := &tfParams{
		tfCmd:     "terraform" + version,
		workspace: workspace,
		path:      path,
	}

	if varsPath != "" {
		ret.varsFile = varsPath
	} else {
		ret.varsFile = fmt.Sprintf("%s.tfvars", ret.workspace)
		fmt.Printf("No vars file provided for %s, using generated value: %s\n", ret.path, ret.varsFile)
	}

	return ret, nil
}

func (c *config) mkConfDirIfNotExist() error {
	if c.tempDir != "" {
		return nil
	}

	var err error
	c.tempDir, err = os.MkdirTemp("", "tf-state-migr-")
	if err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}

	return nil
}

func makeBaseTfCommand(params *tfParams) *exec.Cmd {
	cmd := exec.Command(params.tfCmd)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "TF_WORKSPACE="+params.workspace)
	cmd.Dir = params.path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd
}

func runCmdWrapper(cmd *exec.Cmd) error {
	fmt.Printf("\nRunning command: %v\nWorking directory: %s\n", cmd.Args, cmd.Dir)
	err := cmd.Run()
	fmt.Println("------------END OF TERRAFORM OUTPUT------------\n")
	if err != nil {
		return fmt.Errorf("run command: %w", err)
	}

	return nil
}

func (c *config) generateSvcPlan() error {
	var err error

	// if no json file provided, we need to create one
	if c.targetJsonFile == "" {
		fmt.Println("\nService json file not provided, generating...")
		c.mkConfDirIfNotExist()

		planFile := filepath.Join(c.tempDir, DEFAULT_TARGET_PLAN_FILENAME)

		cmd := makeBaseTfCommand(c.svcTfConf)
		cmd.Args = append(cmd.Args,
			"plan",
			fmt.Sprintf("-out=%s", planFile),
			fmt.Sprintf("-var-file=%s", c.svcTfConf.varsFile),
		)

		err = runCmdWrapper(cmd)
		if err != nil {
			return fmt.Errorf("running terraform plan: %w", err)
		}

		cmd = makeBaseTfCommand(c.svcTfConf)
		cmd.Args = append(
			cmd.Args,
			"show",
			"-json",
			planFile,
		)

		var rawPlan bytes.Buffer
		cmd.Stdout = &rawPlan

		err = runCmdWrapper(cmd)
		if err != nil {
			return fmt.Errorf("converting service plan to json: %w", err)
		}
		rawPlanBytes := rawPlan.Bytes()
		err = json.Unmarshal(rawPlanBytes, &c.svcPlan)
		if err != nil {
			return fmt.Errorf("unmarshaling svc json: %s", err)
		}

		c.targetJsonFile = filepath.Join(c.tempDir, DEFAULT_TARGET_JSON_FIlENAME)
		err = os.WriteFile(c.targetJsonFile, rawPlanBytes, 0644)
		if err != nil {
			return fmt.Errorf("writing service json: %s", err)
		}

		// we already parsed json, exit function
		return nil
	}

	c.svcPlan, err = readPlanFromJsonFile(c.targetJsonFile)
	if err != nil {
		return fmt.Errorf("generate service plan: %w", err)
	}

	return nil
}

func (c *config) getAndWriteLegacyStateJson(tfParams *tfParams, outFileBase string) (*TfState, string, error) {
	var err error
	returnState := new(TfState)
	c.mkConfDirIfNotExist()

	cmd := makeBaseTfCommand(tfParams)
	cmd.Args = append(
		cmd.Args,
		"show",
		"-json",
	)
	var rawStateBuf bytes.Buffer
	cmd.Stdout = &rawStateBuf

	err = runCmdWrapper(cmd)
	if err != nil {
		return nil, "", fmt.Errorf("converting state to json: %w", err)
	}
	// var rawStateBytes []byte
	// copy(rawStateBytes, rawStateBuf.Bytes())
	rawStateBytes := rawStateBuf.Bytes()
	err = json.Unmarshal(rawStateBytes, returnState)

	// resource.index key can be either number (for "count" resources)
	// or string (for "for_each"). We are only interested in strings
	// so we ignore the type error.
	var errTarget *json.UnmarshalTypeError
	if err != nil && !errors.As(err, &errTarget) {
		return nil, "", fmt.Errorf("read state from json: %w", err)
	}

	outFilePath := filepath.Join(c.tempDir, outFileBase)
	err = os.WriteFile(outFilePath, rawStateBytes, 0644)
	if err != nil {
		return nil, "", fmt.Errorf("write registry json to file: %s", err)
	}

	return returnState, outFilePath, nil
}

func (c *config) generateRegistryState() error {
	var err error

	if c.registryJsonFile == "" {
		fmt.Println("\nRegistry json file not provided, generating...")
		c.regState, c.registryJsonFile, err = c.getAndWriteLegacyStateJson(c.regTfConf, DEFAULT_REGISTRY_JSON_FILENAME)
		if err != nil {
			return fmt.Errorf("generate registry state: %w", err)
		}

		// we already parsed json, exit function
		return nil
	}

	c.regState, err = readStateFromJsonFile(c.registryJsonFile)
	if err != nil {
		return fmt.Errorf("generate registry state: %w", err)
	}

	return nil
}

func (c *config) generateClustersState() error {
	var err error
	if c.clustersJsonFile == "" {
		fmt.Println("\nClusters json file not provided, generating...")
		c.clustState, c.clustersJsonFile, err = c.getAndWriteLegacyStateJson(c.clustTfConf, DEFAULT_CLUSTERS_JSON_FILENAME)
		if err != nil {
			return fmt.Errorf("generate clusters state: %w", err)
		}

		// we already parsed json, exit function
		return nil
	}

	c.clustState, err = readStateFromJsonFile(c.clustersJsonFile)
	if err != nil {
		return fmt.Errorf("generate clusters state: %w", err)
	}

	return nil
}

func readPlanFromJsonFile(file string) (*TfPlan, error) {
	planRaw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read plan from json: %w", err)
	}

	plan := new(TfPlan)
	err = json.Unmarshal(planRaw, plan)
	if err != nil {
		return nil, fmt.Errorf("read plan from json: %w", err)
	}

	return plan, nil
}

func readStateFromJsonFile(file string) (*TfState, error) {
	stateRaw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read state from json: %w", err)
	}

	state := new(TfState)
	err = json.Unmarshal(stateRaw, state)

	// resource.index key can be either number (for "count" resources)
	// or string (for "for_each"). We are only interested in strings
	// so we ignore the type error.
	var errTarget *json.UnmarshalTypeError
	if err != nil && !errors.As(err, &errTarget) {
		return nil, fmt.Errorf("read state from json: %w", err)
	}

	return state, nil
}
