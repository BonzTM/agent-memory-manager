package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

type Schema struct {
	Schema               string             `json:"$schema,omitempty"`
	ID                   string             `json:"$id,omitempty"`
	Title                string             `json:"title,omitempty"`
	Description          string             `json:"description,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	AdditionalProperties interface{}        `json:"additionalProperties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	Ref                  string             `json:"$ref,omitempty"`
	Definitions          map[string]*Schema `json:"definitions,omitempty"`
}

var (
	memoryTypes []string
	scopes      []string
	recallModes []string
	privacyLvls []string
	memoryStats []string
	jobKinds    []string
	policyTypes []string
	policyModes []string
	matchModes  []string
)

func parseEnumMapLiteral(expr ast.Expr) []string {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}

	enums := make([]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		k, ok := kv.Key.(*ast.BasicLit)
		if !ok || k.Kind != token.STRING {
			continue
		}

		enums = append(enums, strings.Trim(k.Value, `"`))
	}

	return enums
}

func loadValidationEnums(path string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return fmt.Errorf("failed to parse validation source: %w", err)
	}

	parsed := map[string][]string{}
	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.VAR {
			continue
		}

		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range valueSpec.Names {
				if i >= len(valueSpec.Values) {
					continue
				}
				if enums := parseEnumMapLiteral(valueSpec.Values[i]); len(enums) > 0 {
					parsed[name.Name] = enums
				}
			}
		}
	}

	required := []struct {
		key   string
		target *[]string
	}{
		{key: "validMemoryTypes", target: &memoryTypes},
		{key: "validScopes", target: &scopes},
		{key: "validRecallModes", target: &recallModes},
		{key: "validPrivacyLevels", target: &privacyLvls},
		{key: "validMemoryStatuses", target: &memoryStats},
		{key: "validJobKinds", target: &jobKinds},
		{key: "validPolicyPatternTypes", target: &policyTypes},
		{key: "validPolicyModes", target: &policyModes},
		{key: "validPolicyMatchModes", target: &matchModes},
	}

	missing := []string{}
	for _, item := range required {
		vals := parsed[item.key]
		if len(vals) == 0 {
			missing = append(missing, item.key)
			continue
		}
		*item.target = vals
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("missing enum maps in validation source: %s", strings.Join(missing, ", "))
	}

	return nil
}

func getEnumForField(structName, jsonFieldName string) []string {
	switch structName {
	case "RememberRequest", "UpdateMemoryRequest":
		if jsonFieldName == "type" {
			return memoryTypes
		}
		if jsonFieldName == "scope" {
			return scopes
		}
		if jsonFieldName == "privacy_level" {
			return privacyLvls
		}
		if jsonFieldName == "status" {
			return memoryStats
		}
	case "RecallRequest":
		if jsonFieldName == "mode" {
			return recallModes
		}
	case "IngestEventRequest":
		if jsonFieldName == "privacy_level" {
			return privacyLvls
		}
	case "ShareRequest":
		if jsonFieldName == "privacy" {
			return privacyLvls
		}
	case "ExpandRequest":
		if jsonFieldName == "kind" {
			return []string{"memory", "summary", "episode"}
		}
	case "RunJobRequest":
		if jsonFieldName == "kind" {
			return jobKinds
		}
	case "PolicyAddRequest":
		if jsonFieldName == "pattern_type" {
			return policyTypes
		}
		if jsonFieldName == "mode" {
			return policyModes
		}
		if jsonFieldName == "match_mode" {
			return matchModes
		}
	}
	return nil
}

func parseType(expr ast.Expr) *Schema {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string":
			return &Schema{Type: "string"}
		case "int", "int32", "int64":
			return &Schema{Type: "integer"}
		case "float32", "float64":
			return &Schema{Type: "number"}
		case "bool":
			return &Schema{Type: "boolean"}
		case "interface":
			// interface{}
			return &Schema{Type: "object"} // Usually unstructured object in JSON
		default:
			// Custom struct reference
			return &Schema{Ref: "#/definitions/" + t.Name}
		}
	case *ast.StarExpr:
		// pointer types are optional, base type remains
		return parseType(t.X)
	case *ast.ArrayType:
		// []T
		return &Schema{
			Type:  "array",
			Items: parseType(t.Elt),
		}
	case *ast.MapType:
		// map[string]T
		vType := parseType(t.Value)
		return &Schema{
			Type:                 "object",
			AdditionalProperties: vType,
		}
	case *ast.SelectorExpr:
		// e.g. time.Time or interface{} empty
		if t.Sel.Name == "Time" {
			return &Schema{Type: "string"}
		}
		return &Schema{Type: "object"}
	case *ast.InterfaceType:
		return &Schema{Type: "object", AdditionalProperties: true}
	default:
		return &Schema{Type: "object"} // Fallback
	}
}

func parseJSONTag(tag string) (name string, omitempty bool) {
	if tag == "" {
		return "", false
	}
	// Extract json:"..."
	re := regexp.MustCompile(`json:"([^"]+)"`)
	matches := re.FindStringSubmatch(tag)
	if len(matches) < 2 {
		return "", false
	}
	parts := strings.Split(matches[1], ",")
	name = parts[0]
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}

func main() {
	if err := loadValidationEnums("internal/contracts/v1/validation.go"); err != nil {
		log.Fatalf("Failed to load validation enums: %v", err)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "internal/contracts/v1/payloads.go", nil, parser.ParseComments)
	if err != nil {
		log.Fatalf("Failed to parse payloads.go: %v", err)
	}

	root := &Schema{
		Schema:      "http://json-schema.org/draft-07/schema#",
		ID:          "https://amm.dev/spec/v1/payloads.schema.json",
		Title:       "AMM Payloads",
		Description: "JSON Schema for all AMM request and response payloads",
		Definitions: make(map[string]*Schema),
	}

	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			structName := typeSpec.Name.Name
			schema := &Schema{
				Type:       "object",
				Properties: make(map[string]*Schema),
			}

			var required []string

			for _, field := range structType.Fields.List {
				if field.Tag == nil {
					continue
				}

				tagValue := reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("json")
				if tagValue == "" {
					continue
				}

				name, omitempty := parseJSONTag(field.Tag.Value)
				if name == "" || name == "-" {
					continue
				}

				if !omitempty {
					required = append(required, name)
				}

				propSchema := parseType(field.Type)

				if enum := getEnumForField(structName, name); enum != nil {
					propSchema.Enum = enum
				}

				schema.Properties[name] = propSchema
			}

			if len(required) > 0 {
				schema.Required = required
			}

			root.Definitions[structName] = schema
		}
	}

	// Create output file
	if err := os.MkdirAll("spec/v1", 0755); err != nil {
		log.Fatalf("Failed to create output dir: %v", err)
	}

	out, err := os.Create("spec/v1/payloads.schema.json")
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer out.Close()

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		log.Fatalf("Failed to encode JSON Schema: %v", err)
	}

	fmt.Printf("Successfully generated schema for %d payloads to spec/v1/payloads.schema.json\n", len(root.Definitions))
}
