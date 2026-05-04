package simulator

import (
	"math/rand"
	"strings"

	"github.com/fookiejs/fookie/pkg/ast"
	"github.com/google/uuid"
)

func inputFieldName(f *ast.Field) string {
	if f.Type == ast.TypeRelation {
		return f.Name + "_id"
	}
	return f.Name
}

func fieldRequired(f *ast.Field) bool {
	for _, v := range f.Validators {
		if v.Name == "required" {
			return true
		}
	}
	return false
}

func validatorNumArg(f *ast.Field, name string) (float64, bool) {
	for _, v := range f.Validators {
		if v.Name != name {
			continue
		}
		switch x := v.Arg.(type) {
		case float64:
			return x, true
		case int:
			return float64(x), true
		case int64:
			return float64(x), true
		default:
			return 0, false
		}
	}
	return 0, false
}

func validatorStrArg(f *ast.Field, name string) (string, bool) {
	for _, v := range f.Validators {
		if v.Name == name && v.Arg != nil {
			if s, ok := v.Arg.(string); ok {
				return s, true
			}
		}
	}
	return "", false
}

func enumPick(schema *ast.Schema, enumRef string, rng *rand.Rand) string {
	for _, en := range schema.Enums {
		if en.Name == enumRef && len(en.Values) > 0 {
			return en.Values[rng.Intn(len(en.Values))]
		}
	}
	return "unknown_enum"
}

func randPatternString(pattern string, minLen, maxLen int, rng *rand.Rand) string {
	upper := strings.Contains(pattern, "A-Z") || strings.Contains(pattern, "[A-Z")
	digit := strings.Contains(pattern, "0-9") || strings.Contains(pattern, "\\d")
	underscore := strings.Contains(pattern, "_")
	chars := "abcdefghijklmnopqrstuvwxyz"
	if upper {
		chars += "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	}
	if digit {
		chars += "0123456789"
	}
	if underscore {
		chars += "_"
	}
	if chars == "" {
		chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	}
	n := minLen
	if maxLen > minLen {
		n += rng.Intn(maxLen - minLen + 1)
	}
	if n < 1 {
		n = 4
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(chars[rng.Intn(len(chars))])
	}
	return b.String()
}

func valueForField(schema *ast.Schema, f *ast.Field, valid bool, rng *rand.Rand) interface{} {
	if !valid && rng.Float32() < 0.25 {
		switch f.Type {
		case ast.TypeNumber:
			return "not_a_number"
		case ast.TypeBoolean:
			return "not_bool"
		case ast.TypeEnum:
			return "__invalid_enum__"
		default:
			return rng.Float64() * 100
		}
	}

	switch f.Type {
	case ast.TypeBoolean:
		return rng.Intn(2) == 1
	case ast.TypeNumber:
		minV := -1e6
		maxV := 1e6
		if mn, ok := validatorNumArg(f, "min"); ok {
			minV = mn
		}
		if mx, ok := validatorNumArg(f, "max"); ok {
			maxV = mx
		}
		for _, v := range f.Validators {
			if v.Name == "positive" {
				minV = 1
			}
		}
		if maxV <= minV {
			maxV = minV + 100
		}
		return minV + rng.Float64()*(maxV-minV)
	case ast.TypeEnum:
		if f.EnumRef != nil {
			v := enumPick(schema, *f.EnumRef, rng)
			if !valid && rng.Float32() < 0.3 {
				return v + "_bad"
			}
			return v
		}
		return "e"
	case ast.TypeRelation, ast.TypeID, ast.TypeUUID:
		return uuid.New().String()
	case ast.TypeJSON:
		return "{}"
	default:
		minLen := 1
		maxLen := 24
		if mn, ok := validatorNumArg(f, "min"); ok {
			minLen = int(mn)
			if minLen < 1 {
				minLen = 1
			}
		}
		if mx, ok := validatorNumArg(f, "max"); ok {
			maxLen = int(mx)
			if maxLen < minLen {
				maxLen = minLen + 8
			}
		}
		pat, hasPat := validatorStrArg(f, "pattern")
		if hasPat && strings.Contains(pat, "A-Z") {
			return randPatternString(pat, minLen, maxLen, rng)
		}
		s := randPatternString("", minLen, maxLen, rng)
		for _, v := range f.Validators {
			if v.Name == "notEmpty" && s == "" {
				s = "x"
			}
		}
		return s
	}
}

func BuildCreateBody(schema *ast.Schema, m *ast.Model, valid bool, rng *rand.Rand) map[string]interface{} {
	body := map[string]interface{}{}
	var omit string
	if !valid {
		var candidates []string
		for _, f := range m.Fields {
			if fieldRequired(f) {
				candidates = append(candidates, inputFieldName(f))
			}
		}
		if len(candidates) > 0 {
			omit = candidates[rng.Intn(len(candidates))]
		}
	}
	for _, f := range m.Fields {
		key := inputFieldName(f)
		if key == omit {
			continue
		}
		if f.Default != nil && !fieldRequired(f) && rng.Float32() < 0.4 {
			body[key] = f.Default
			continue
		}
		body[key] = valueForField(schema, f, valid, rng)
	}
	return body
}

func BuildUpdatePatch(schema *ast.Schema, m *ast.Model, valid bool, rng *rand.Rand) map[string]interface{} {
	body := map[string]interface{}{}
	n := 1 + rng.Intn(3)
	if n > len(m.Fields) {
		n = len(m.Fields)
	}
	indices := rng.Perm(len(m.Fields))
	for i := 0; i < n && i < len(indices); i++ {
		f := m.Fields[indices[i]]
		key := inputFieldName(f)
		body[key] = valueForField(schema, f, valid, rng)
	}
	return body
}

func filterEqID(id string) map[string]interface{} {
	return map[string]interface{}{
		"id": map[string]interface{}{"eq": id},
	}
}

func filterEqFirstStringField(m *ast.Model, val string) map[string]interface{} {
	for _, f := range m.Fields {
		switch f.Type {
		case ast.TypeString, ast.TypeEmail, ast.TypeURL:
			return map[string]interface{}{
				f.Name: map[string]interface{}{"eq": val},
			}
		default:
			continue
		}
	}
	return map[string]interface{}{}
}
