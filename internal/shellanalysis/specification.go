package shellanalysis

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/internal/permission"
)

// The checked-in specification is compiled once. Repository files and command
// --help output never become permission rules at runtime.
//
//go:embed commands.json
var commandSpecification []byte

type optionSpec struct {
	Names []string `json:"names"`
	Value bool     `json:"value"`
	Role  string   `json:"role"`
}

type commandSpec struct {
	Names         []string                `json:"names"`
	Handler       string                  `json:"handler"`
	Operation     string                  `json:"operation"`
	Capability    permission.Capability   `json:"capability"`
	Builtin       bool                    `json:"builtin"`
	Opaque        bool                    `json:"opaque"`
	StopAtOperand bool                    `json:"stop_at_operand"`
	DefaultPath   string                  `json:"default_path"`
	Options       []optionSpec            `json:"options"`
	Subcommands   map[string]*commandSpec `json:"subcommands"`
	options       map[string]optionSpec
}

var commandSpecs, specificationError = compileSpecifications(commandSpecification)
var specificationDigest = fmt.Sprintf("%x", sha256.Sum256(append(append([]byte{}, commandSpecification...), pathSpecification...)))

func compileSpecifications(data []byte) (map[string]*commandSpec, error) {
	var specs []*commandSpec
	if err := json.Unmarshal(data, &specs, json.RejectUnknownMembers(true)); err != nil {
		return nil, err
	}
	result := make(map[string]*commandSpec)
	var prepare func(*commandSpec) error
	prepare = func(spec *commandSpec) error {
		if err := validateHandler(spec.Handler); err != nil {
			return err
		}
		spec.options = make(map[string]optionSpec)
		for _, opt := range spec.Options {
			for _, name := range opt.Names {
				if !strings.HasPrefix(name, "-") {
					return fmt.Errorf("invalid option %q", name)
				}
				if _, exists := spec.options[name]; exists {
					return fmt.Errorf("duplicate option %q", name)
				}
				spec.options[name] = opt
			}
		}
		for _, sub := range spec.Subcommands {
			if err := prepare(sub); err != nil {
				return err
			}
		}
		return nil
	}
	for _, spec := range specs {
		if err := prepare(spec); err != nil {
			return nil, err
		}
		for _, name := range spec.Names {
			if _, exists := result[name]; exists {
				return nil, fmt.Errorf("duplicate command %q", name)
			}
			result[name] = spec
		}
	}
	return result, nil
}

type optionValue struct{ role, value string }
type parsedArgs struct {
	operands []string
	options  []optionValue
	unknown  bool
}

// Parse only declared options: clusters, attached values, --name=value and --.
// Unknown options never acquire semantics from a shared positional heuristic.
func parseOptions(spec *commandSpec, args []string) parsedArgs {
	out := parsedArgs{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			out.operands = append(out.operands, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			out.operands = append(out.operands, arg)
			if spec.StopAtOperand {
				out.operands = append(out.operands, args[i+1:]...)
				break
			}
			continue
		}
		name, attached, equal := strings.Cut(arg, "=")
		if opt, ok := spec.options[name]; ok {
			value := attached
			if opt.Value && !equal {
				if i+1 >= len(args) {
					out.unknown = true
					continue
				}
				i++
				value = args[i]
			} else if !opt.Value && equal {
				out.unknown = true
				continue
			}
			out.options = append(out.options, optionValue{opt.Role, value})
			continue
		}
		if strings.HasPrefix(arg, "--") {
			out.unknown = true
			continue
		}
		for j := 1; j < len(arg); j++ {
			opt, ok := spec.options["-"+arg[j:j+1]]
			if !ok {
				out.unknown = true
				break
			}
			value := ""
			if opt.Value {
				value = arg[j+1:]
				if value == "" {
					if i+1 >= len(args) {
						out.unknown = true
						break
					}
					i++
					value = args[i]
				}
			}
			out.options = append(out.options, optionValue{opt.Role, value})
			if opt.Value {
				break
			}
		}
	}
	return out
}

func (p parsedArgs) has(role string) bool {
	for _, opt := range p.options {
		if opt.role == role {
			return true
		}
	}
	return false
}

func (p parsedArgs) last(role string) string {
	for i := len(p.options) - 1; i >= 0; i-- {
		if p.options[i].role == role {
			return p.options[i].value
		}
	}
	return ""
}
