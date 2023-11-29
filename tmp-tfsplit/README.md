# TFSplit project state migration

This script/app is developed to help ease the toil when migrating state for
[TFSplit migration project](https://gocardless.atlassian.net/browse/CI-2199).

## Compilation

If your host system is Apple silicon, you might need to cross-compile to run the binary in the
`amd64` atlantis container provided by `utopia terraform`:

You can use Makefile for this:
```
# Compile for native architecture
make native

# Compile for amd64
make amd64

# Compile for all (default target)
make
```

The resulting files will be found in `bin/`

## Usage

Before usage, copy the compiled binary to a path which will be available in
Terraform container, for example, the `anu` directory in which you will be
running `utopia terraform`.

Simlpest use-case is to execute program from `utopia terraform` running
container, while in the `terraform/infra` directory of the utopia service being
migrated, for example,
`/anu/utopia/services/playground/gjonasts-playground/terraform/infra`.

Default options should be correct for first run, but see below for
useful time savers.

The mandatory positional arguments are as follows:
```
tfsplit-amd64 <legacy-projects-workspace> <target-project-workspace>
```

## Improving execution time for consecutive runs

Several options exist to skip performing redundant tasks when running the application multiple times.

### Saving json output for future runs

To avoid doing expensive terraform state reads and plans when running multiple times,
the script output files can be kept:

```
tfsplit-amd64 -keep-dir <legacy-projects-workspace> <target-project-workspace>
```

The temporary directory path will be output at the end of the run. See next sections on how to use these.

### Using pre-generated json output

To avoid regenerating json data from plan/state, you can reuse existing output.
The useful files generated are:

- `clusters-state.json` - json output of the `clusters` project. Can usually be reused between runs.
- `registry-state.json` - json output of the `registry` project. Can usually be reused between runs.
- `target-plan.json` - json output of target plan. Only reuse this if running for the same target service environment.

You possibly might also want to skip terraform initialization step with
`-skip-init` if the projects are already initialized.

```
tfsplit-amd64 -clusters-json /path/of/clusters-state.json -registry-json /path/of/registry-state.json -target-json /path/of/target-plan.json -skip-init <legacy-projects-workspace> <target-project-workspace>
```

## tfvars file

By default, the `.tfvars` file name is generated from workspace.
- For clusters/registry project, generated path `<workspace>.tfvars`.
- For target service project, generated path is `../environments/<workspace>.tfvars`

The file path must be relative to the project directory.

It can be overriden with following options:
```
-clusters-vars-file string
    Clusters project tfvars relative path (default generated from workspace)

-registry-vars-file string
    Registry project tfvars relative path (default generated from workspace)

-target-vars-file string
    Target service project tfvars relative path (default generated from workspace)
```

## Terraform version

Different Terraform versions can be used for legacy and target projects.
The Terraform command executed conforms to our `utopia terraform` available
Terraform commands in form of `terraform<version>`.

By default, Terraform version used is `0.13.7` for clusters/registry projects,
and `1.5.2` for target project.

This can be overriden with:

```
-legacy-tf-version string
    Terraform version for legacy projects (default "0.13.7")
-target-tf-version string
    Terraform version for target service project (default "1.5.2")
```

# List of optional options

Full list of optional options for the program:
```
tfsplit-script [options] <legacy-workspace> <target-workspace>
Usage of tfsplit-script:
  -clusters-dir string
    	Clusters Terraform project directory (default "/anu/terraform/google/projects/clusters")
  -clusters-json string
    	(optional) clusters TF state json file
  -clusters-vars-file string
    	Clusters project tfvars relative path (default generated from workspace)
  -keep-dir
    	Keep temporary directory created
  -legacy-tf-version string
    	Terraform version for legacy projects (default "0.13.7")
  -registry-dir string
    	Registry Terraform project directory (default "/anu/terraform/google/projects/registry")
  -registry-json string
    	(optional) registry TF state json file
  -registry-vars-file string
    	Registry project tfvars relative path (default generated from workspace)
  -skip-init
    	Skip Terraform initializations (must be initialized before)
  -target-dir string
    	Target Terraform project directory (defaults to CWD)
  -target-json string
    	(optional) target TF plan json file
  -target-tf-version string
    	Terraform version for target service project (default "1.5.2")
  -target-vars-file string
    	Target service project tfvars relative path (default generated from workspace)
```
