package tftmpl

import (
	"fmt"
	"io"
	"log"

	"github.com/hashicorp/consul-terraform-sync/templates/hcltmpl"
	"github.com/hashicorp/hcat/dep"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

type healthService struct {
	ID        string            `hcl:"id"`
	Name      string            `hcl:"name"`
	Address   string            `hcl:"address"`
	Port      int               `hcl:"port"`
	Meta      map[string]string `hcl:"meta"`
	Tags      []string          `hcl:"tags"`
	Namespace cty.Value         `hcl:"namespace"`
	Status    string            `hcl:"status"`

	Node                string            `hcl:"node"`
	NodeID              string            `hcl:"node_id"`
	NodeAddress         string            `hcl:"node_address"`
	NodeDatacenter      string            `hcl:"node_datacenter"`
	NodeTaggedAddresses map[string]string `hcl:"node_tagged_addresses"`
	NodeMeta            map[string]string `hcl:"node_meta"`
}

func newHealthService(s *dep.HealthService) healthService {
	if s == nil {
		return healthService{}
	}

	// Namespace is null-able
	var namespace cty.Value
	if s.Namespace != "" {
		namespace = cty.StringVal(s.Namespace)
	} else {
		namespace = cty.NullVal(cty.String)
	}

	// Default to empty list instead of null
	tags := []string{}
	if s.Tags != nil {
		tags = s.Tags
	}

	return healthService{
		ID:        s.ID,
		Name:      s.Name,
		Address:   s.Address,
		Port:      s.Port,
		Meta:      nonNullMap(s.ServiceMeta),
		Tags:      tags,
		Namespace: namespace,
		Status:    s.Status,

		Node:                s.Node,
		NodeID:              s.NodeID,
		NodeAddress:         s.NodeAddress,
		NodeDatacenter:      s.NodeDatacenter,
		NodeTaggedAddresses: nonNullMap(s.NodeTaggedAddresses),
		NodeMeta:            nonNullMap(s.NodeMeta),
	}
}

// NewTFVarsTmpl writes content to assign values to the root module's variables
// that is commonly placed in a .tfvars file.
func NewTFVarsTmpl(w io.Writer, input *RootModuleInputData) error {
	_, err := w.Write(RootPreamble)
	if err != nil {
		// This isn't required for TF config files to be usable. So we'll just log
		// the error and continue.
		log.Printf("[WARN] (templates.tftmpl) unable to write preamble warning to %q",
			TFVarsTmplFilename)
	}

	hclFile := hclwrite.NewEmptyFile()
	body := hclFile.Body()
	appendNamedBlockValues(body, input.Providers)
	body.AppendNewline()
	appendRawServiceTemplateValues(body, input.Services)

	_, err = hclFile.WriteTo(w)
	return err
}

// appendNamedBlockValues appends blocks that assign value to the named
// variable blocks genernated by `appendNamedBlockVariable`
func appendNamedBlockValues(body *hclwrite.Body, blocks []hcltmpl.NamedBlock) {
	lastIdx := len(blocks) - 1
	for i, b := range blocks {
		obj := b.ObjectVal()
		body.SetAttributeValue(b.Name, *obj)
		if i != lastIdx {
			body.AppendNewline()
		}
	}
}

// appendRawServiceTemplateValues appends raw lines representing blocks that
// assign value to the services variable `VariableServices` with `hcat` template
// syntax for dynamic rendering of Consul dependency values.
//
// services = {
//   <service>: {
//	   <attr> = <value>
//     <attr> = {{ <template syntax> }}
//   }
// }
func appendRawServiceTemplateValues(body *hclwrite.Body, services []Service) {
	if len(services) == 0 {
		return
	}

	tokens := make([]*hclwrite.Token, 0, len(services)+2)
	tokens = append(tokens, &hclwrite.Token{
		Type:  hclsyntax.TokenOBrace,
		Bytes: []byte("{"),
	})
	lastIdx := len(services) - 1
	for i, s := range services {
		rawService := fmt.Sprintf(baseAddressStr, s.TemplateServiceID())

		if i == lastIdx {
			rawService += "\n}"
		} else {
			nextS := services[i+1]
			rawComma := fmt.Sprintf(baseCommaStr, s.TemplateServiceID(),
				nextS.TemplateServiceID())
			rawService += rawComma
		}

		token := hclwrite.Token{
			Type:  hclsyntax.TokenNil,
			Bytes: []byte(rawService),
		}
		tokens = append(tokens, &token)
	}
	body.SetAttributeRaw("services", tokens)
}

func nonNullMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}

	return m
}

// baseAddressStr is the raw template following hcat syntax for addresses of
// Consul services.
const baseAddressStr = `
{{- with $srv := service "%s"}}
  {{- $last := len $srv | subtract 1}}
  {{- range $i, $s := $srv}}
  "{{ joinStrings "." .ID .Node .Namespace .NodeDatacenter }}" : {
{{ HCLService $s | indent 4 }}
  } {{- if (ne $i $last)}},{{end}}
  {{- end}}
{{- end}}`

// baseCommaStr is the raw template following hcat syntax for the comma between
// different Consul services. Rendering a comma requires there to be an instance
// of the service before and after the comma.
const baseCommaStr = `{{- with $beforeSrv := service "%s"}}
  {{- with $afterSrv := service "%s"}},{{end}}
{{- end}}`
