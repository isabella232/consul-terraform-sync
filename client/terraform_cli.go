package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// workspaceEnv is the environment variable to set a Terraform workspace
const workspaceEnv = "TF_WORKSPACE"

var _ Client = (*TerraformCLI)(nil)

// TerraformCLI is the client that wraps around terraform-exec
// to execute Terraform cli commands
type TerraformCLI struct {
	tf         terraformExec
	logLevel   string
	workingDir string
	workspace  string
}

// TerraformCLIConfig configures the Terraform client
type TerraformCLIConfig struct {
	LogLevel   string
	ExecPath   string
	WorkingDir string
	Workspace  string
}

// NewTerraformCLI creates a terraform-exec client and configures and
// initializes a new Terraform client
func NewTerraformCLI(config *TerraformCLIConfig) (*TerraformCLI, error) {
	if config == nil {
		return nil, errors.New("TerraformCLIConfig cannot be nil - no meaningful default values")
	}

	tf, err := tfexec.NewTerraform(config.WorkingDir, config.ExecPath)
	if err != nil {
		return nil, err
	}
	if config.Workspace != "" {
		env := make(map[string]string)
		env[workspaceEnv] = config.Workspace
		tf.SetEnv(env)
	}

	return &TerraformCLI{
		tf:         tf,
		logLevel:   config.LogLevel,
		workingDir: config.WorkingDir,
		workspace:  config.Workspace,
	}, nil
}

// Init executes the cli command a `terraform init`
func (t *TerraformCLI) Init(ctx context.Context) error {
	return t.tf.Init(ctx)
}

// Apply executes the cli command `terraform apply` for a given workspace
func (t *TerraformCLI) Apply(ctx context.Context) error {
	return t.tf.Apply(ctx)
}

// Plan executes the cli command a `terraform plan` for a given workspace
func (t *TerraformCLI) Plan(ctx context.Context) error {
	return t.tf.Plan(ctx)
}

// GoString defines the printable version of this struct.
func (t *TerraformCLI) GoString() string {
	if t == nil {
		return "(*TerraformCLI)(nil)"
	}

	return fmt.Sprintf("&TerraformCLI{"+
		"LogLevel:%s, "+
		"WorkingDir:%s, "+
		"WorkSpace:%s, "+
		"}",
		t.logLevel,
		t.workingDir,
		t.workspace,
	)
}