// Package plan turns a bound query into a logical/physical tree (Phase 6)
// and attaches row-group skip decisions (Phase 7).
package plan

import (
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/exec"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/kernel"
)

const (
	OpScan    = "ParquetScan"
	OpFilter  = "Filter"
	OpProject = "Project"
	OpAgg     = "HashAggregate"
	OpSort    = "Sort"
	OpLimit   = "Limit"
	OpEmpty   = "Empty"
)

// Input is the binder output (SQL or flag-based agg).
type Input struct {
	Table    *catalog.Table
	Where    expr.Expr
	ScanCols []string
	GroupBy  []string
	Aggs     []kernel.AggSpec
	Project  []string
	Order    []kernel.OrderKey
	Limit    int64
	IsAgg    bool
	NoSkip   bool
	NoPrune  bool
}

// Node is one operator. Children are nested (unary pipeline).
type Node struct {
	Op    string
	Child *Node

	Table    string
	TableRef *catalog.Table
	Keep     []string
	Pushed   expr.Expr
	Residual expr.Expr
	GroupBy  []string
	Aggs     []kernel.AggSpec
	Project  []string
	Order    []kernel.OrderKey
	Limit    int64

	Files            int
	RowGroupsTotal   int
	RowGroupsKept    int
	RowGroupsSkipped int
	KeepByFile       map[string][]int
	Empty            bool
	PrunedCols       int
	BytesRead        int64
	RowsRead         int64
	Analyze          bool
	Jobs             int
}

// Build constructs Scan→Filter→Agg/Project→Sort→Limit and applies optimizer rules.
func Build(in Input) *Node {
	n := buildLogical(in)
	n = optimize(n, in)
	if !in.NoSkip {
		applySkip(n)
	}
	return n
}

// SetJobs records the worker count on every node (shown in EXPLAIN).
func (n *Node) SetJobs(j int) {
	for cur := n; cur != nil; cur = cur.Child {
		cur.Jobs = j
	}
}

func buildLogical(in Input) *Node {
	keep := append([]string(nil), in.ScanCols...)
	scan := &Node{
		Op:       OpScan,
		Table:    "",
		TableRef: in.Table,
		Keep:     keep,
	}
	if in.Table != nil {
		scan.Table = in.Table.Name
		scan.Files = len(in.Table.Files)
		scan.RowGroupsTotal = in.Table.NumRowGroups
		scan.RowGroupsKept = in.Table.NumRowGroups
	}
	n := scan
	if in.Where != nil {
		n = &Node{Op: OpFilter, Residual: in.Where, Child: n}
	}
	if in.IsAgg || len(in.Aggs) > 0 || len(in.GroupBy) > 0 {
		n = &Node{Op: OpAgg, GroupBy: append([]string(nil), in.GroupBy...), Aggs: append([]kernel.AggSpec(nil), in.Aggs...), Project: append([]string(nil), in.Project...), Child: n}
	} else if len(in.Project) > 0 {
		n = &Node{Op: OpProject, Project: append([]string(nil), in.Project...), Child: n}
	}
	if len(in.Order) > 0 {
		n = &Node{Op: OpSort, Order: append([]kernel.OrderKey(nil), in.Order...), Child: n}
	}
	if in.Limit > 0 {
		n = &Node{Op: OpLimit, Limit: in.Limit, Child: n}
	}
	return n
}

// Request lowers the physical tree to the Phase-4 pipeline.
func (n *Node) Request(batchSize int64) exec.Request {
	scan := n.find(OpScan)
	req := exec.Request{BatchSize: batchSize}
	if scan != nil && scan.TableRef != nil {
		req.Table = scan.TableRef
		req.ScanCols = append([]string(nil), scan.Keep...)
		req.RowGroups = scan.KeepByFile
	}
	if f := n.find(OpFilter); f != nil && f.Residual != nil {
		req.Where = f.Residual
	} else if scan != nil && scan.Pushed != nil {
		req.Where = scan.Pushed
	}
	// If we pushed some conjuncts and left a residual, the filter node holds
	// the residual only — still apply pushed preds after the scan.
	if scan != nil && scan.Pushed != nil && req.Where != scan.Pushed {
		req.Where = andExprs(scan.Pushed, req.Where)
	}
	if a := n.find(OpAgg); a != nil {
		req.GroupBy = append([]string(nil), a.GroupBy...)
		req.Aggs = append([]kernel.AggSpec(nil), a.Aggs...)
		req.Project = append([]string(nil), a.Project...)
	}
	if p := n.find(OpProject); p != nil {
		req.Project = append([]string(nil), p.Project...)
	}
	if s := n.find(OpSort); s != nil {
		req.Order = append([]kernel.OrderKey(nil), s.Order...)
	}
	if l := n.find(OpLimit); l != nil {
		req.Limit = l.Limit
	}
	if n.Op == OpEmpty || (scan != nil && scan.Empty) {
		req.Empty = true
	}
	return req
}

func (n *Node) find(op string) *Node {
	for cur := n; cur != nil; cur = cur.Child {
		if cur.Op == op {
			return cur
		}
	}
	return nil
}

func andExprs(a, b expr.Expr) expr.Expr {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &expr.Binary{Op: expr.OpAnd, Left: a, Right: b}
}
