package tftmpl

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/hashicorp/consul-terraform-sync/templates/hcltmpl"
	"github.com/hashicorp/consul-terraform-sync/version"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/hashicorp/terraform/configs/hcl2shim"
	"github.com/zclconf/go-cty/cty"
)

const (
	// TerraformRequiredVersion is the version constraint pinned to the generated
	// root module to ensure compatibility across Sync, Terraform, and
	// modules.
	TerraformRequiredVersion = version.CompatibleTerraformVersionConstraint

	// RootFilename is the file name for the root module.
	RootFilename = "main.tf"

	// VarsFilename is the file name for the variable definitions in the root module
	VarsFilename = "variables.tf"

	// ModuleVarsFilename is the file name for the variable definitions corresponding
	// to the input variables from a user that is specific to the task's module.
	ModuleVarsFilename = "variables.module.tf"

	// TFVarsFilename is the file name for input variables for configured
	// Terraform providers and Consul service information.
	TFVarsFilename = "terraform.tfvars"

	// TFVarsTmplFilename is the template file for TFVarsFilename
	TFVarsTmplFilename = "terraform.tfvars.tmpl"
)

var (
	// RootPreamble is a warning message included to the beginning of the
	// generated root module files.
	RootPreamble = []byte(
		`# This file is generated by Consul Terraform Sync.
#
# The HCL blocks, arguments, variables, and values are derived from the
# operator configuration for Sync. Any manual changes to this file
# may not be preserved and could be overwritten by a subsequent update.

`)

	rootFileFuncs = map[string]func(io.Writer, *RootModuleInputData) error{
		RootFilename:       NewMainTF,
		VarsFilename:       NewVariablesTF,
		ModuleVarsFilename: NewModuleVariablesTF,
		TFVarsTmplFilename: NewTFVarsTmpl,
	}
)

// Task contains information for a Sync task. The Terraform driver
// interprets task values for determining the Terraform module.
type Task struct {
	Description string
	Name        string
	Source      string
	Version     string
}

type Service struct {
	Datacenter  string
	Description string
	Name        string
	Namespace   string
	Tag         string
}

// TODO incorporate namespace
func (s Service) TemplateServiceID() string {
	id := s.Name

	if s.Tag != "" {
		id = fmt.Sprintf("%s.%s", s.Tag, s.Name)
	}

	if s.Datacenter != "" {
		id = fmt.Sprintf("%s@%s", id, s.Datacenter)
	}

	return id
}

// RootModuleInputData is the input data used to generate the root module
type RootModuleInputData struct {
	Backend      map[string]interface{}
	Providers    []hcltmpl.NamedBlock
	ProviderInfo map[string]interface{}
	Services     []Service
	Task         Task
	Variables    hcltmpl.Variables

	backend *hcltmpl.NamedBlock
}

// Init processes input data used to generate a Terraform root module. It
// converts the RootModuleInputData values into HCL objects compatible for
// Terraform configuration syntax.
func (d *RootModuleInputData) Init() {
	if d.Backend != nil {
		block := hcltmpl.NewNamedBlock(d.Backend)
		d.backend = &block
	} else {
		d.Backend = make(map[string]interface{})
	}

	sort.Slice(d.Providers, func(i, j int) bool {
		return d.Providers[i].Name < d.Providers[j].Name
	})

	sort.Slice(d.Services, func(i, j int) bool {
		return d.Services[i].Name < d.Services[j].Name
	})
}

// InitRootModule generates the root module and writes the following files to
// disk: main.tf, variables.tf
func InitRootModule(input *RootModuleInputData, modulePath string, filePerms os.FileMode, force bool) error {
	for filename, newFileFunc := range rootFileFuncs {
		if filename == ModuleVarsFilename && len(input.Variables) == 0 {
			// Skip variables.module.tf if there are no user input variables
			continue
		}

		filePath := filepath.Join(modulePath, filename)
		exists := fileExists(filePath)
		switch {
		case exists && !force:
			log.Printf("[DEBUG] (templates.tftmpl) %s in root module for task %q "+
				"already exists, skipping file creation", filename, input.Task.Name)

		case exists && force:
			log.Printf("[INFO] (templates.tftmpl) overwriting %s in root module "+
				"for task %q", filename, input.Task.Name)
			fallthrough

		default:
			log.Printf("[DEBUG] (templates.tftmpl) creating %s in root module for "+
				"task %q: %s", filename, input.Task.Name, filePath)

			f, err := os.Create(filePath)
			if err != nil {
				log.Printf("[ERR] (templates.tftmpl) unable to create %s in root "+
					"module for %q: %s", filename, input.Task.Name, err)
				return err
			}
			defer f.Close()

			if err := f.Chmod(filePerms); err != nil {
				log.Printf("[ERR] (templates.tftmpl) unable to change permissions "+
					"for %s in root module for %q: %s", filename, input.Task.Name, err)
				return err
			}

			if err := newFileFunc(f, input); err != nil {
				log.Printf("[ERR] (templates.tftmpl) error writing content for %s in "+
					"root module for %q: %s", filename, input.Task.Name, err)
				return err
			}

			f.Sync()
		}
	}

	return nil
}

// NewMainTF writes content used for main.tf of a Terraform root module.
func NewMainTF(w io.Writer, input *RootModuleInputData) error {
	_, err := w.Write(RootPreamble)
	if err != nil {
		// This isn't required for TF config files to be usable. So we'll just log
		// the error and continue.
		log.Printf("[WARN] (templates.tftmpl) unable to write preamble warning to %q",
			RootFilename)
	}

	hclFile := hclwrite.NewEmptyFile()
	rootBody := hclFile.Body()
	appendRootTerraformBlock(rootBody, input.backend, input.ProviderInfo)
	rootBody.AppendNewline()
	appendRootProviderBlocks(rootBody, input.Providers)
	rootBody.AppendNewline()
	appendRootModuleBlock(rootBody, input.Task, input.Variables.Keys())

	// Format the file before writing
	content := hclFile.Bytes()
	content = hclwrite.Format(content)
	_, err = w.Write(content)
	return err
}

// appendRootTerraformBlock appends the Terraform block with version constraint
// and backend.
func appendRootTerraformBlock(body *hclwrite.Body, backend *hcltmpl.NamedBlock,
	providerInfo map[string]interface{}) {

	tfBlock := body.AppendNewBlock("terraform", nil)
	tfBody := tfBlock.Body()
	tfBody.SetAttributeValue("required_version", cty.StringVal(TerraformRequiredVersion))

	if len(providerInfo) != 0 {
		requiredProvidersBody := tfBody.AppendNewBlock("required_providers", nil).Body()
		for _, pName := range sortedKeys(providerInfo) {
			info, ok := providerInfo[pName]
			if ok {
				requiredProvidersBody.SetAttributeValue(pName, hcl2shim.HCL2ValueFromConfigValue(info))
			}
		}
	}

	// Configure the Terraform backend within the Terraform block
	if backend == nil || backend.Name == "" {
		return
	}
	backendBody := tfBody.AppendNewBlock("backend", []string{backend.Name}).Body()
	backendAttrs := backend.SortedAttributes()
	for _, attr := range backendAttrs {
		backendBody.SetAttributeValue(attr, backend.Variables[attr])
	}
}

// appendRootProviderBlocks appends Terraform provider blocks for the providers
// the task requires.
func appendRootProviderBlocks(body *hclwrite.Body, providers []hcltmpl.NamedBlock) {
	lastIdx := len(providers) - 1
	for i, p := range providers {
		providerBody := body.AppendNewBlock("provider", []string{p.Name}).Body()

		// Convert user provider attr+values to provider block arguments from variables
		// and sort the attributes for consistency
		// attr = var.<providerName>.<attr>
		providerAttrs := p.SortedAttributes()
		for _, attr := range providerAttrs {
			// Drop the alias meta attribute. Each provider instance will be ran as
			// a separate task
			if attr == "alias" {
				continue
			}

			providerBody.SetAttributeTraversal(attr, hcl.Traversal{
				hcl.TraverseRoot{Name: "var"},
				hcl.TraverseAttr{Name: p.Name},
				hcl.TraverseAttr{Name: attr},
			})
		}
		if i != lastIdx {
			body.AppendNewline()
		}
	}
}

// appendRootModuleBlock appends a Terraform module block for the task
func appendRootModuleBlock(body *hclwrite.Body, task Task, varNames []string) {
	// Add user description for task above the module block
	if task.Description != "" {
		appendComment(body, task.Description)
	}

	moduleBlock := body.AppendNewBlock("module", []string{task.Name})
	moduleBody := moduleBlock.Body()
	moduleBody.SetAttributeValue("source", cty.StringVal(task.Source))
	if len(task.Version) > 0 {
		moduleBody.SetAttributeValue("version", cty.StringVal(task.Version))
	}

	moduleBody.SetAttributeTraversal("services", hcl.Traversal{
		hcl.TraverseRoot{Name: "var"},
		hcl.TraverseAttr{Name: "services"},
	})

	if len(varNames) != 0 {
		moduleBody.AppendNewline()
	}
	for _, name := range varNames {
		moduleBody.SetAttributeTraversal(name, hcl.Traversal{
			hcl.TraverseRoot{Name: "var"},
			hcl.TraverseAttr{Name: name},
		})
	}
}

// appendComment appends a single HCL comment line
func appendComment(b *hclwrite.Body, comment string) {
	b.AppendUnstructuredTokens(hclwrite.Tokens{{
		Type:  hclsyntax.TokenComment,
		Bytes: []byte(fmt.Sprintf("# %s", comment)),
	}})
	b.AppendNewline()
}

func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]interface{}) []string {
	sorted := make([]string, 0, len(m))
	for key := range m {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	return sorted
}
