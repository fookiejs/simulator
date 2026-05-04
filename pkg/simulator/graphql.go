package simulator

import (
	"strings"

	"github.com/fookiejs/fookie/pkg/ast"
)

type Request struct {
	Query         string
	Variables     map[string]interface{}
	OpLabel       string
	ExpectDataKey string
}

func selectionLine(names []string) string {
	if len(names) == 0 {
		return "id"
	}
	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(n)
	}
	return b.String()
}

func BuildCreateRequest(m *ast.Model, body map[string]interface{}) Request {
	sn := toSnake(m.Name)
	bt := "Create" + m.Name + "Body"
	q := `mutation($b: ` + bt + `!) { create_` + sn + `(body: $b) { id } }`
	return Request{
		Query:         q,
		Variables:     map[string]interface{}{"b": body},
		OpLabel:       "create_" + sn,
		ExpectDataKey: "create_" + sn,
	}
}

func BuildUpdateRequest(m *ast.Model, id string, patch map[string]interface{}) Request {
	sn := toSnake(m.Name)
	bt := "Update" + m.Name + "Body"
	q := `mutation($id: ID!, $b: ` + bt + `!) { update_` + sn + `(id: $id, body: $b) { id } }`
	return Request{
		Query:         q,
		Variables:     map[string]interface{}{"id": id, "b": patch},
		OpLabel:       "update_" + sn,
		ExpectDataKey: "update_" + sn,
	}
}

func BuildDeleteRequest(m *ast.Model, id string) Request {
	sn := toSnake(m.Name)
	q := `mutation($id: ID!) { delete_` + sn + `(id: $id) }`
	return Request{
		Query:         q,
		Variables:     map[string]interface{}{"id": id},
		OpLabel:       "delete_" + sn,
		ExpectDataKey: "delete_" + sn,
	}
}

func BuildReadRequest(m *ast.Model, op *ast.Operation, filter map[string]interface{}) Request {
	sn := toSnake(m.Name)
	ft := m.Name + "FilterInput"
	sel := readSelectionFields(m, op)
	line := selectionLine(sel)
	var q string
	var vars map[string]interface{}
	if len(filter) == 0 {
		q = `query { all_` + sn + ` { ` + line + ` } }`
		vars = nil
	} else {
		q = `query($f: ` + ft + `) { all_` + sn + `(filter: $f) { ` + line + ` } }`
		vars = map[string]interface{}{"f": filter}
	}
	return Request{
		Query:         q,
		Variables:     vars,
		OpLabel:       "all_" + sn,
		ExpectDataKey: "all_" + sn,
	}
}

func BuildReadAggregateRequest(m *ast.Model, op *ast.Operation, filter map[string]interface{}) Request {
	sn := toSnake(m.Name)
	ft := m.Name + "FilterInput"
	sel := aggregateSelection(op)
	line := selectionLine(sel)
	var q string
	var vars map[string]interface{}
	if len(filter) == 0 {
		q = `query { all_` + sn + ` { ` + line + ` } }`
		vars = nil
	} else {
		q = `query($f: ` + ft + `) { all_` + sn + `(filter: $f) { ` + line + ` } }`
		vars = map[string]interface{}{"f": filter}
	}
	return Request{
		Query:         q,
		Variables:     vars,
		OpLabel:       "all_" + sn + "_agg",
		ExpectDataKey: "all_" + sn,
	}
}

func BuildAggregateScalarRequest(m *ast.Model, opType string, field string, filter map[string]interface{}) Request {
	sn := toSnake(m.Name)
	ft := m.Name + "FilterInput"
	var fieldName string
	if opType == "count" {
		fieldName = "count_" + sn
	} else {
		fieldName = opType + "_" + sn + "_" + toSnake(field)
	}
	var q string
	var vars map[string]interface{}
	if len(filter) == 0 {
		q = `query { ` + fieldName + ` }`
		vars = nil
	} else {
		q = `query($f: ` + ft + `) { ` + fieldName + `(filter: $f) }`
		vars = map[string]interface{}{"f": filter}
	}
	return Request{
		Query:         q,
		Variables:     vars,
		OpLabel:       fieldName,
		ExpectDataKey: fieldName,
	}
}

func readSelectionFields(m *ast.Model, op *ast.Operation) []string {
	if op != nil && isAggregateRead(op) {
		return aggregateSelection(op)
	}
	var out []string
	if op != nil {
		for _, sf := range op.Select {
			if pf, ok := sf.Expr.(*ast.PlainField); ok && len(pf.Path) > 0 {
				out = append(out, pf.Path[0])
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	out = append(out, "id")
	n := 0
	for _, f := range m.Fields {
		if n >= 5 {
			break
		}
		switch f.Type {
		case ast.TypeRelation:
			out = append(out, inputFieldName(f))
		case ast.TypeJSON:
			continue
		default:
			out = append(out, f.Name)
			n++
		}
	}
	return out
}
