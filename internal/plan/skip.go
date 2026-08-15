package plan

import (
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/parquetscan"
)

func applySkip(n *Node) {
	if n == nil {
		return
	}
	if n.Op == OpEmpty {
		return
	}
	scan := n.find(OpScan)
	if scan == nil || scan.TableRef == nil {
		return
	}
	pred := scan.Pushed
	if pred == nil {
		if f := n.find(OpFilter); f != nil {
			pred = f.Residual
		}
	} else if f := n.find(OpFilter); f != nil && f.Residual != nil {
		pred = andExprs(pred, f.Residual)
	}
	tbl := scan.TableRef
	scan.RowGroupsTotal = tbl.NumRowGroups
	scan.Files = len(tbl.Files)
	if pred == nil {
		scan.RowGroupsKept = tbl.NumRowGroups
		scan.RowGroupsSkipped = 0
		return
	}
	keep := map[string][]int{}
	kept, skipped := 0, 0
	for _, rg := range tbl.RowGroups {
		if canSkip(rg, pred) {
			skipped++
			continue
		}
		keep[rg.File] = append(keep[rg.File], rg.Index)
		kept++
	}
	// files with no kept groups still need an empty slice so the reader skips them
	for _, f := range tbl.Files {
		if _, ok := keep[f]; !ok {
			keep[f] = []int{}
		}
	}
	scan.KeepByFile = keep
	scan.RowGroupsKept = kept
	scan.RowGroupsSkipped = skipped
	if kept == 0 {
		scan.Empty = true
	}
}

// canSkip reports whether rg is guaranteed to contain no rows matching pred.
func canSkip(rg catalog.RowGroup, pred expr.Expr) bool {
	skip, _ := skipPred(rg, pred)
	return skip
}

func skipPred(rg catalog.RowGroup, pred expr.Expr) (skip bool, ok bool) {
	if pred == nil {
		return false, true
	}
	switch n := pred.(type) {
	case *expr.Binary:
		switch n.Op {
		case expr.OpAnd:
			lSkip, lOK := skipPred(rg, n.Left)
			rSkip, rOK := skipPred(rg, n.Right)
			if (lOK && lSkip) || (rOK && rSkip) {
				return true, true
			}
			return false, lOK && rOK
		case expr.OpOr:
			lSkip, lOK := skipPred(rg, n.Left)
			rSkip, rOK := skipPred(rg, n.Right)
			if lOK && rOK && lSkip && rSkip {
				return true, true
			}
			return false, lOK && rOK
		case expr.OpEq, expr.OpNe, expr.OpLt, expr.OpLe, expr.OpGt, expr.OpGe:
			return skipCompare(rg, n)
		}
	case *expr.InList:
		return skipIn(rg, n)
	case *expr.Between:
		return skipBetween(rg, n)
	case *expr.IsNull:
		return skipIsNull(rg, n)
	case *expr.Unary:
		// Conservative: NOT is not used to skip (except we don't try).
		return false, true
	case *expr.Lit:
		if n.Kind == expr.LitBool {
			if !n.Bool {
				return true, true
			}
			return false, true
		}
	}
	return false, false
}

func skipCompare(rg catalog.RowGroup, n *expr.Binary) (bool, bool) {
	col, lit, op, ok := colLitOp(n)
	if !ok {
		return false, false
	}
	st, found := rg.ColStats(col)
	if !found || !st.HasMinMax {
		return false, true
	}
	b, ok := litBound(lit, st.MinBound.Kind)
	if !ok {
		return false, false
	}
	cmpMin, err1 := st.MinBound.Cmp(b)
	cmpMax, err2 := st.MaxBound.Cmp(b)
	if err1 != nil || err2 != nil {
		return false, false
	}
	switch op {
	case expr.OpGt: // skip if max <= k
		return cmpMax <= 0, true
	case expr.OpGe: // skip if max < k
		return cmpMax < 0, true
	case expr.OpLt: // skip if min >= k
		return cmpMin >= 0, true
	case expr.OpLe: // skip if min > k
		return cmpMin > 0, true
	case expr.OpEq: // skip if k < min or k > max
		return cmpMin > 0 || cmpMax < 0, true
	case expr.OpNe: // skip if min = max = k
		return cmpMin == 0 && cmpMax == 0, true
	}
	return false, true
}

func colLitOp(n *expr.Binary) (col string, lit *expr.Lit, op expr.Op, ok bool) {
	if c, cok := n.Left.(*expr.Col); cok {
		if l, lok := n.Right.(*expr.Lit); lok {
			return c.Name, l, n.Op, true
		}
	}
	if c, cok := n.Right.(*expr.Col); cok {
		if l, lok := n.Left.(*expr.Lit); lok {
			return c.Name, l, swapOp(n.Op), true
		}
	}
	return "", nil, expr.OpInvalid, false
}

func swapOp(op expr.Op) expr.Op {
	switch op {
	case expr.OpLt:
		return expr.OpGt
	case expr.OpLe:
		return expr.OpGe
	case expr.OpGt:
		return expr.OpLt
	case expr.OpGe:
		return expr.OpLe
	default:
		return op
	}
}

func skipIn(rg catalog.RowGroup, n *expr.InList) (bool, bool) {
	c, ok := n.X.(*expr.Col)
	if !ok {
		return false, false
	}
	st, found := rg.ColStats(c.Name)
	if !found || !st.HasMinMax {
		return false, true
	}
	anyOverlap := false
	for _, v := range n.Vals {
		b, ok := litBound(v, st.MinBound.Kind)
		if !ok {
			return false, false
		}
		cmpMin, err1 := st.MinBound.Cmp(b)
		cmpMax, err2 := st.MaxBound.Cmp(b)
		if err1 != nil || err2 != nil {
			return false, false
		}
		if cmpMin <= 0 && cmpMax >= 0 {
			anyOverlap = true
		}
	}
	if n.Not {
		// NOT IN: skip only if the group is a single value in the list.
		return false, true
	}
	return !anyOverlap, true
}

func skipBetween(rg catalog.RowGroup, n *expr.Between) (bool, bool) {
	c, ok := n.X.(*expr.Col)
	if !ok {
		return false, false
	}
	st, found := rg.ColStats(c.Name)
	if !found || !st.HasMinMax {
		return false, true
	}
	lo, ok1 := litBound(n.Low, st.MinBound.Kind)
	hi, ok2 := litBound(n.High, st.MinBound.Kind)
	if !ok1 || !ok2 {
		return false, false
	}
	cmpMaxLo, err1 := st.MaxBound.Cmp(lo) // max vs low
	cmpMinHi, err2 := st.MinBound.Cmp(hi) // min vs high
	if err1 != nil || err2 != nil {
		return false, false
	}
	// skip if max < low or min > high
	outside := cmpMaxLo < 0 || cmpMinHi > 0
	if n.Not {
		// skip if the whole group is inside [low, high]
		cmpMinLo, _ := st.MinBound.Cmp(lo)
		cmpMaxHi, _ := st.MaxBound.Cmp(hi)
		return cmpMinLo >= 0 && cmpMaxHi <= 0, true
	}
	return outside, true
}

func skipIsNull(rg catalog.RowGroup, n *expr.IsNull) (bool, bool) {
	c, ok := n.X.(*expr.Col)
	if !ok {
		return false, false
	}
	st, found := rg.ColStats(c.Name)
	if !found || st.NullCount == nil {
		return false, true
	}
	if n.Not {
		return *st.NullCount == st.NumValues, true
	}
	return *st.NullCount == 0, true
}

func litBound(l *expr.Lit, kind parquetscan.BoundKind) (parquetscan.Bound, bool) {
	if l == nil {
		return parquetscan.Bound{}, false
	}
	switch l.Kind {
	case expr.LitInt:
		if kind == parquetscan.BoundInt64 || kind == parquetscan.BoundNone {
			return parquetscan.Bound{Kind: parquetscan.BoundInt64, I64: l.I64}, true
		}
		if kind == parquetscan.BoundFloat64 {
			return parquetscan.Bound{Kind: parquetscan.BoundFloat64, F64: float64(l.I64)}, true
		}
	case expr.LitFloat:
		if kind == parquetscan.BoundFloat64 {
			return parquetscan.Bound{Kind: parquetscan.BoundFloat64, F64: l.F64}, true
		}
	case expr.LitString:
		if kind == parquetscan.BoundBytes {
			return parquetscan.Bound{Kind: parquetscan.BoundBytes, Bytes: []byte(l.Str)}, true
		}
	case expr.LitBool:
		if kind == parquetscan.BoundBool {
			return parquetscan.Bound{Kind: parquetscan.BoundBool, Bool: l.Bool}, true
		}
	}
	return parquetscan.Bound{}, false
}
