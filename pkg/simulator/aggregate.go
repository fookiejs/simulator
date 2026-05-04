package simulator

import (
	"strings"

	"github.com/fookiejs/fookie/pkg/ast"
)

func isAggregateRead(op *ast.Operation) bool {
	if op == nil {
		return false
	}
	for _, sf := range op.Select {
		if _, ok := sf.Expr.(*ast.AggregateFunc); ok {
			return true
		}
	}
	return false
}

func aggregateSelection(op *ast.Operation) []string {
	var out []string
	for _, sf := range op.Select {
		alias := sf.Alias
		if alias == "" {
			if af, ok := sf.Expr.(*ast.AggregateFunc); ok {
				j := strings.Join(af.Field, "")
				if j == "" {
					alias = af.Fn
				} else {
					alias = af.Fn + strings.ToUpper(j[:1]) + j[1:]
				}
			}
		}
		if alias != "" {
			out = append(out, alias)
		}
	}
	return out
}
