package simulator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/fookiejs/fookie/pkg/ast"
)

type IDPool struct {
	byModel map[string][]string
}

func NewIDPool() *IDPool {
	return &IDPool{byModel: map[string][]string{}}
}

func (p *IDPool) Push(model, id string) {
	if id == "" {
		return
	}
	p.byModel[model] = append(p.byModel[model], id)
}

func (p *IDPool) Random(model string, rng *rand.Rand) (string, bool) {
	slice := p.byModel[model]
	if len(slice) == 0 {
		return "", false
	}
	i := rng.Intn(len(slice))
	id := slice[i]
	slice[i] = slice[len(slice)-1]
	p.byModel[model] = slice[:len(slice)-1]
	return id, true
}

func (p *IDPool) PeekRandom(model string, rng *rand.Rand) (string, bool) {
	slice := p.byModel[model]
	if len(slice) == 0 {
		return "", false
	}
	return slice[rng.Intn(len(slice))], true
}

func (p *IDPool) TotalIDs() int {
	n := 0
	for _, s := range p.byModel {
		n += len(s)
	}
	return n
}

type Runner struct {
	Schema     *ast.Schema
	BaseURL    string
	AuthBearer string
	ValidRatio float64
	RNG        *rand.Rand
	Pool       *IDPool
	HTTP       *http.Client
}

func NewRunner(schema *ast.Schema, baseURL string, seed int64, validRatio float64) *Runner {
	return &Runner{
		Schema:     schema,
		BaseURL:    baseURL,
		ValidRatio: validRatio,
		RNG:        rand.New(rand.NewSource(seed)),
		Pool:       NewIDPool(),
		HTTP: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func modelEligible(m *ast.Model) bool {
	return len(m.CRUD) > 0
}

func (r *Runner) randomValid() bool {
	return r.RNG.Float64() < r.ValidRatio
}

func (r *Runner) pickModel() *ast.Model {
	var eligible []*ast.Model
	for _, m := range r.Schema.Models {
		if modelEligible(m) {
			eligible = append(eligible, m)
		}
	}
	if len(eligible) == 0 {
		return nil
	}
	return eligible[r.RNG.Intn(len(eligible))]
}

type gqlResp struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (r *Runner) post(req Request) (gqlResp, error) {
	var out gqlResp
	payload := map[string]interface{}{"query": req.Query}
	if len(req.Variables) > 0 {
		payload["variables"] = req.Variables
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return out, err
	}
	hreq, err := http.NewRequest(http.MethodPost, r.BaseURL, bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if r.AuthBearer != "" {
		hreq.Header.Set("Authorization", "Bearer "+r.AuthBearer)
	}
	res, err := r.HTTP.Do(hreq)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("decode: %w body=%s", err, string(raw))
	}
	return out, nil
}

func parseCreatedID(data json.RawMessage, key string) (string, bool) {
	if len(data) == 0 {
		return "", false
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrap); err != nil {
		return "", false
	}
	inner, ok := wrap[key]
	if !ok {
		return "", false
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(inner, &obj); err != nil {
		return "", false
	}
	id, ok := obj["id"].(string)
	return id, ok
}

func graphqlErrMsg(resp gqlResp) string {
	if len(resp.Errors) == 0 {
		return ""
	}
	return resp.Errors[0].Message
}

func (r *Runner) execCreate(m *ast.Model) (string, bool, string) {
	valid := r.randomValid()
	body := BuildCreateBody(r.Schema, m, valid, r.RNG)
	req := BuildCreateRequest(m, body)
	resp, err := r.post(req)
	if err != nil {
		return req.OpLabel, false, err.Error()
	}
	if msg := graphqlErrMsg(resp); msg != "" {
		return req.OpLabel, false, msg
	}
	id, ok := parseCreatedID(resp.Data, req.ExpectDataKey)
	if ok {
		r.Pool.Push(m.Name, id)
	}
	return req.OpLabel, true, ""
}

func (r *Runner) execUpdate(m *ast.Model) (string, bool, string) {
	id, ok := r.Pool.PeekRandom(m.Name, r.RNG)
	if !ok {
		return "", false, "skip-no-id"
	}
	valid := r.randomValid()
	patch := BuildUpdatePatch(r.Schema, m, valid, r.RNG)
	req := BuildUpdateRequest(m, id, patch)
	resp, err := r.post(req)
	if err != nil {
		return req.OpLabel, false, err.Error()
	}
	if msg := graphqlErrMsg(resp); msg != "" {
		return req.OpLabel, false, msg
	}
	return req.OpLabel, true, ""
}

func (r *Runner) execDelete(m *ast.Model) (string, bool, string) {
	id, ok := r.Pool.Random(m.Name, r.RNG)
	if !ok {
		return "", false, "skip-no-id"
	}
	req := BuildDeleteRequest(m, id)
	resp, err := r.post(req)
	if err != nil {
		r.Pool.Push(m.Name, id)
		return req.OpLabel, false, err.Error()
	}
	if msg := graphqlErrMsg(resp); msg != "" {
		r.Pool.Push(m.Name, id)
		return req.OpLabel, false, msg
	}
	return req.OpLabel, true, ""
}

func (r *Runner) execRead(m *ast.Model) (string, bool, string) {
	op := m.CRUD["read"]
	if op == nil {
		return "", false, "skip"
	}
	var filter map[string]interface{}
	if isAggregateRead(op) {
		if len(aggregateSelection(op)) == 0 {
			return "", false, "skip"
		}
		if id, ok := r.Pool.PeekRandom(m.Name, r.RNG); ok && r.RNG.Float32() < 0.5 {
			filter = filterEqID(id)
		}
		req := BuildReadAggregateRequest(m, op, filter)
		resp, err := r.post(req)
		if err != nil {
			return req.OpLabel, false, err.Error()
		}
		if msg := graphqlErrMsg(resp); msg != "" {
			return req.OpLabel, false, msg
		}
		return req.OpLabel, true, ""
	}
	if id, ok := r.Pool.PeekRandom(m.Name, r.RNG); ok && r.RNG.Float32() < 0.45 {
		filter = filterEqID(id)
	} else if r.RNG.Float32() < 0.15 {
		filter = filterEqFirstStringField(m, fmt.Sprintf("sim-%d", r.RNG.Int63()))
	}
	req := BuildReadRequest(m, op, filter)
	resp, err := r.post(req)
	if err != nil {
		return req.OpLabel, false, err.Error()
	}
	if msg := graphqlErrMsg(resp); msg != "" {
		return req.OpLabel, false, msg
	}
	return req.OpLabel, true, ""
}

func (r *Runner) execAggregateScalar(m *ast.Model) (string, bool, string) {
	opType, field, ok := pickAggregateScalarOp(m)
	if !ok {
		return "", false, "skip"
	}
	var filter map[string]interface{}
	if id, ok2 := r.Pool.PeekRandom(m.Name, r.RNG); ok2 && r.RNG.Float32() < 0.4 {
		filter = filterEqID(id)
	}
	req := BuildAggregateScalarRequest(m, opType, field, filter)
	resp, err := r.post(req)
	if err != nil {
		return req.OpLabel, false, err.Error()
	}
	if msg := graphqlErrMsg(resp); msg != "" {
		return req.OpLabel, false, msg
	}
	return req.OpLabel, true, ""
}

func pickAggregateScalarOp(m *ast.Model) (string, string, bool) {
	for ot, op := range m.CRUD {
		switch ot {
		case "sum", "avg", "min", "max", "stddev", "variance":
			return ot, op.Field, true
		case "count":
			return ot, "", true
		default:
		}
	}
	return "", "", false
}

type weighted struct {
	w int
	run func() (string, bool, string)
}

func (r *Runner) Step() (label string, ok bool, detail string) {
	m := r.pickModel()
	if m == nil {
		return "", false, "no-models"
	}
	var shots []weighted
	if m.CRUD["create"] != nil {
		shots = append(shots, weighted{5, func() (string, bool, string) { return r.execCreate(m) }})
	}
	if m.CRUD["read"] != nil {
		shots = append(shots, weighted{5, func() (string, bool, string) { return r.execRead(m) }})
	}
	if _, _, hasAgg := pickAggregateScalarOp(m); hasAgg {
		shots = append(shots, weighted{3, func() (string, bool, string) { return r.execAggregateScalar(m) }})
	}
	if m.CRUD["update"] != nil {
		if _, okp := r.Pool.PeekRandom(m.Name, r.RNG); okp {
			shots = append(shots, weighted{3, func() (string, bool, string) { return r.execUpdate(m) }})
		}
	}
	if m.CRUD["delete"] != nil {
		if _, okp := r.Pool.PeekRandom(m.Name, r.RNG); okp {
			shots = append(shots, weighted{1, func() (string, bool, string) { return r.execDelete(m) }})
		}
	}
	if len(shots) == 0 {
		return "", false, "no-op"
	}
	total := 0
	for _, s := range shots {
		total += s.w
	}
	roll := r.RNG.Intn(total)
	for _, s := range shots {
		if roll < s.w {
			return s.run()
		}
		roll -= s.w
	}
	return shots[len(shots)-1].run()
}
