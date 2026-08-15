package plan

import (
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
)

func optimize(n *Node, in Input) *Node {
	n = foldConstants(n)
	n = pushPredicates(n)
	n = pruneColumns(n, in)
	n = pushLimit(n)
	return n
}

func foldConstants(n *Node) *Node {
	if n == nil {
		return nil
	}
	n.Child = foldConstants(n.Child)
	if n.Op == OpFilter {
		n.Residual = foldExpr(n.Residual)
		if isFalse(n.Residual) {
			return emptyFrom(n)
		}
		if isTrue(n.Residual) {
			return n.Child
		}
	}
	return n
}

func emptyFrom(n *Node) *Node {
	scan := n.find(OpScan)
	e := &Node{Op: OpEmpty, Empty: true}
	if scan != nil {
		e.Table = scan.Table
		e.TableRef = scan.TableRef
		e.Keep = scan.Keep
		e.Files = scan.Files
		e.RowGroupsTotal = scan.RowGroupsTotal
		e.RowGroupsSkipped = scan.RowGroupsTotal
		e.Child = &Node{
			Op:               OpScan,
			Table:            scan.Table,
			TableRef:         scan.TableRef,
			Keep:             scan.Keep,
			Files:            scan.Files,
			RowGroupsTotal:   scan.RowGroupsTotal,
			RowGroupsKept:    0,
			RowGroupsSkipped: scan.RowGroupsTotal,
			Empty:            true,
			KeepByFile:       map[string][]int{},
		}
		if scan.TableRef != nil {
			m := map[string][]int{}
			for _, f := range scan.TableRef.Files {
				m[f] = []int{}
			}
			e.Child.KeepByFile = m
		}
	}
	return e
}

func foldExpr(e expr.Expr) expr.Expr {
	if e == nil {
		return nil
	}
	switch n := e.(type) {
	case *expr.Binary:
		left, right := foldExpr(n.Left), foldExpr(n.Right)
		switch n.Op {
		case expr.OpAnd:
			if isFalse(left) || isFalse(right) {
				return boolLit(false)
			}
			if isTrue(left) {
				return right
			}
			if isTrue(right) {
				return left
			}
			return &expr.Binary{Op: expr.OpAnd, Left: left, Right: right}
		case expr.OpOr:
			if isTrue(left) || isTrue(right) {
				return boolLit(true)
			}
			if isFalse(left) {
				return right
			}
			if isFalse(right) {
				return left
			}
			return &expr.Binary{Op: expr.OpOr, Left: left, Right: right}
		default:
			return &expr.Binary{Op: n.Op, Left: left, Right: right}
		}
	case *expr.Unary:
		x := foldExpr(n.X)
		if n.Op == expr.OpNot {
			if isTrue(x) {
				return boolLit(false)
			}
			if isFalse(x) {
				return boolLit(true)
			}
		}
		return &expr.Unary{Op: n.Op, X: x}
	default:
		return e
	}
}

func boolLit(v bool) expr.Expr {
	return &expr.Lit{Kind: expr.LitBool, Bool: v}
}

func isTrue(e expr.Expr) bool {
	l, ok := e.(*expr.Lit)
	return ok && l.Kind == expr.LitBool && l.Bool
}

func isFalse(e expr.Expr) bool {
	l, ok := e.(*expr.Lit)
	return ok && l.Kind == expr.LitBool && !l.Bool
}

func pushPredicates(n *Node) *Node {
	if n == nil {
		return nil
	}
	n.Child = pushPredicates(n.Child)
	if n.Op != OpFilter || n.Residual == nil {
		return n
	}
	scan := n.find(OpScan)
	if scan == nil {
		return n
	}
	var push, rest []expr.Expr
	for _, c := range flattenAnd(n.Residual) {
		if isPushable(c) {
			push = append(push, c)
		} else {
			rest = append(rest, c)
		}
	}
	scan.Pushed = andList(push)
	if len(rest) == 0 {
		return n.Child
	}
	n.Residual = andList(rest)
	return n
}

func flattenAnd(e expr.Expr) []expr.Expr {
	if e == nil {
		return nil
	}
	b, ok := e.(*expr.Binary)
	if ok && b.Op == expr.OpAnd {
		return append(flattenAnd(b.Left), flattenAnd(b.Right)...)
	}
	return []expr.Expr{e}
}

func andList(parts []expr.Expr) expr.Expr {
	if len(parts) == 0 {
		return nil
	}
	acc := parts[0]
	for i := 1; i < len(parts); i++ {
		acc = &expr.Binary{Op: expr.OpAnd, Left: acc, Right: parts[i]}
	}
	return acc
}

func isPushable(e expr.Expr) bool {
	switch n := e.(type) {
	case *expr.Binary:
		switch n.Op {
		case expr.OpAnd, expr.OpOr:
			return isPushable(n.Left) && isPushable(n.Right)
		case expr.OpEq, expr.OpNe, expr.OpLt, expr.OpLe, expr.OpGt, expr.OpGe:
			return isColLit(n.Left, n.Right) || isColLit(n.Right, n.Left)
		}
	case *expr.InList:
		_, ok := n.X.(*expr.Col)
		return ok
	case *expr.Between:
		_, ok := n.X.(*expr.Col)
		return ok
	case *expr.IsNull:
		_, ok := n.X.(*expr.Col)
		return ok
	case *expr.Unary:
		return n.Op == expr.OpNot && isPushable(n.X)
	case *expr.Lit:
		return n.Kind == expr.LitBool
	}
	return false
}

func isColLit(a, b expr.Expr) bool {
	_, c := a.(*expr.Col)
	_, l := b.(*expr.Lit)
	return c && l
}

func pruneColumns(n *Node, in Input) *Node {
	scan := n.find(OpScan)
	if scan == nil || scan.TableRef == nil {
		return n
	}
	if in.NoPrune {
		names := make([]string, 0, len(scan.TableRef.Fields))
		for _, f := range scan.TableRef.Fields {
			names = append(names, f.Name)
		}
		scan.Keep = names
		scan.PrunedCols = 0
		return n
	}
	keep := map[string]struct{}{}
	add := func(cols []string) {
		for _, c := range cols {
			if c != "" {
				keep[c] = struct{}{}
			}
		}
	}
	add(expr.Columns(in.Where))
	add(in.GroupBy)
	for _, a := range in.Aggs {
		if a.Input != "" {
			keep[a.Input] = struct{}{}
		}
	}
	if !in.IsAgg && len(in.Aggs) == 0 && len(in.GroupBy) == 0 {
		add(in.Project)
		for _, k := range in.Order {
			keep[k.Name] = struct{}{}
		}
	}
	if len(keep) == 0 && len(scan.TableRef.Fields) > 0 {
		keep[scan.TableRef.Fields[0].Name] = struct{}{}
	}
	var names []string
	for _, f := range scan.TableRef.Fields {
		if _, ok := keep[f.Name]; ok {
			names = append(names, f.Name)
		}
	}
	scan.Keep = names
	scan.PrunedCols = len(scan.TableRef.Fields) - len(names)
	if scan.PrunedCols < 0 {
		scan.PrunedCols = 0
	}
	return n
}

func pushLimit(n *Node) *Node {
	if n == nil || n.Op != OpLimit {
		return n
	}
	// Only push when there is no ORDER BY / GROUP BY between Limit and Scan.
	for cur := n.Child; cur != nil; cur = cur.Child {
		switch cur.Op {
		case OpSort, OpAgg:
			return n
		}
	}
	if scan := n.find(OpScan); scan != nil {
		scan.Limit = n.Limit
	}
	return n
}
