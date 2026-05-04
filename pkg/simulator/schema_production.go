package simulator

import (
	"strings"

	"github.com/fookiejs/fookie/pkg/ast"
)

func SchemaProductionMode(s *ast.Schema) bool {
	if s == nil {
		return false
	}
	for _, c := range s.Configs {
		if !strings.EqualFold(c.Key, "production") {
			continue
		}
		if c.Type != ast.TypeBoolean {
			continue
		}
		b, ok := c.Value.(bool)
		return ok && b
	}
	return false
}
