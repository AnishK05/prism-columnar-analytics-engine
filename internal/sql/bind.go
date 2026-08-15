package sql

import (
	"fmt"
	"strings"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/exec"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/kernel"
)

// BoundQuery is a catalog-checked SELECT ready for the Phase-4 pipeline.
type BoundQuery struct {
	Table    *catalog.Table
	Where    expr.Expr
	GroupBy  []string
	Aggs     []kernel.AggSpec
	Project  []string
	Order    []kernel.OrderKey
	Limit    int64
	IsAgg    bool
	ScanCols []string
	AST      *Query
}

// Bind resolves names against the catalog and validates GROUP BY / types.
func Bind(q *Query, cat *catalog.Catalog) (*BoundQuery, error) {
	if q == nil {
		return nil, fmt.Errorf("nil query")
	}
	tbl, err := cat.Table(q.From)
	if err != nil {
		return nil, err
	}
	b := &BoundQuery{Table: tbl, AST: q}
	if q.Limit != nil {
		if *q.Limit < 0 {
			return nil, fmt.Errorf("LIMIT must be >= 0")
		}
		b.Limit = *q.Limit
	}

	if q.Where != nil {
		if err := checkWhere(q.Where, tbl); err != nil {
			return nil, err
		}
		pred, err := ToPred(q.Where)
		if err != nil {
			return nil, err
		}
		b.Where = pred
	}

	hasStar := false
	hasAgg := false
	hasNonAgg := false
	type outCol struct {
		name  string
		agg   *kernel.AggSpec
		input string // group/scan column
	}
	var outs []outCol
	used := map[string]int{}
	uniq := func(base string) string {
		if base == "" {
			base = "col"
		}
		n := used[base]
		used[base] = n + 1
		if n == 0 {
			return base
		}
		return fmt.Sprintf("%s_%d", base, n+1)
	}

	for _, it := range q.Items {
		if it.Star {
			if it.Alias != "" {
				return nil, fmt.Errorf("cannot alias *")
			}
			hasStar = true
			hasNonAgg = true
			for _, f := range tbl.Fields {
				outs = append(outs, outCol{name: uniq(f.Name), input: f.Name})
			}
			continue
		}
		if it.Expr == nil {
			return nil, fmt.Errorf("empty select item")
		}
		if isArith(it.Expr) {
			return nil, fmt.Errorf("SELECT arithmetic not implemented in v1")
		}
		switch e := it.Expr.(type) {
		case *Call:
			hasAgg = true
			spec, err := bindAgg(e, tbl)
			if err != nil {
				return nil, err
			}
			name := it.Alias
			if name == "" {
				name = spec.Name
			}
			spec.Name = uniq(name)
			outs = append(outs, outCol{name: spec.Name, agg: &spec})
		case *Ident:
			hasNonAgg = true
			if _, ok := tbl.FieldType(e.Name); !ok {
				return nil, fmt.Errorf("unknown column %q", e.Name)
			}
			name := it.Alias
			if name == "" {
				name = e.Name
			}
			outs = append(outs, outCol{name: uniq(name), input: e.Name})
		case *Literal:
			return nil, fmt.Errorf("bare literals in SELECT are not executed in v1")
		default:
			return nil, fmt.Errorf("unsupported select expression %T", it.Expr)
		}
	}

	if hasStar && hasAgg {
		return nil, fmt.Errorf("SELECT * cannot be mixed with aggregates")
	}
	if hasAgg && hasNonAgg && len(q.GroupBy) == 0 {
		return nil, fmt.Errorf("column must appear in GROUP BY or an aggregate")
	}

	gbNames := map[string]struct{}{}
	for _, g := range q.GroupBy {
		id, ok := g.(*Ident)
		if !ok {
			return nil, fmt.Errorf("GROUP BY must be a column name in v1")
		}
		if _, ok := tbl.FieldType(id.Name); !ok {
			return nil, fmt.Errorf("unknown column %q in GROUP BY", id.Name)
		}
		b.GroupBy = append(b.GroupBy, id.Name)
		gbNames[id.Name] = struct{}{}
	}

	if hasAgg || len(b.GroupBy) > 0 {
		b.IsAgg = true
		for _, o := range outs {
			if o.agg == nil {
				if _, ok := gbNames[o.input]; !ok {
					return nil, fmt.Errorf("column %q must appear in GROUP BY or an aggregate", o.input)
				}
			}
		}
	}

	// HashAgg emits keys in GROUP BY order, then aggs in select-agg order.
	// Reorder: we ask HashAgg for group keys + all aggs, then Project to SELECT order.
	if b.IsAgg {
		keySet := map[string]struct{}{}
		for _, k := range b.GroupBy {
			keySet[k] = struct{}{}
		}
		for _, o := range outs {
			if o.agg != nil {
				b.Aggs = append(b.Aggs, *o.agg)
			} else if _, ok := keySet[o.input]; !ok {
				// already validated
			}
		}
		// If a group key is selected under an alias, Project uses agg output names
		// which are the original key names from HashAgg. Then we need aliases...
		// Keep HashAgg output names = group key names + agg.Name, then Project by
		// mapping select order onto those names. Aliased group keys: Project can't
		// rename. For v1, group key output name is the column name; alias on a
		// group column is applied by Project list using the key name (alias ignored
		// unless we rename). Simple approach: Project to select names by building
		// a record with aliased fields in exec — skip, use original names unless
		// agg alias.
		for _, o := range outs {
			if o.agg != nil {
				b.Project = append(b.Project, o.agg.Name)
			} else {
				b.Project = append(b.Project, o.input)
			}
		}
	} else {
		for _, o := range outs {
			b.Project = append(b.Project, o.input)
			b.ScanCols = append(b.ScanCols, o.input)
		}
	}

	aliasToOut := map[string]string{}
	for i, it := range q.Items {
		if it.Star {
			continue
		}
		if i >= len(b.Project) {
			break
		}
		if it.Alias != "" {
			aliasToOut[it.Alias] = b.Project[i]
		}
		switch e := it.Expr.(type) {
		case *Ident:
			aliasToOut[e.Name] = b.Project[i]
		case *Call:
			aliasToOut[e.String()] = b.Project[i]
			aliasToOut[strings.ToUpper(e.String())] = b.Project[i]
		}
	}

	for _, o := range q.OrderBy {
		name, err := resolveOrder(o.Expr, tbl, aliasToOut, b)
		if err != nil {
			return nil, err
		}
		b.Order = append(b.Order, kernel.OrderKey{Name: name, Desc: o.Desc})
	}

	b.ScanCols = unionScan(b.ScanCols, expr.Columns(b.Where), b.GroupBy)
	for _, a := range b.Aggs {
		if a.Input != "" {
			b.ScanCols = append(b.ScanCols, a.Input)
		}
	}
	return b, nil
}

func bindAgg(c *Call, tbl *catalog.Table) (kernel.AggSpec, error) {
	name := strings.ToUpper(c.Name)
	spec := kernel.AggSpec{}
	switch name {
	case "COUNT":
		if c.Star {
			spec.Fn = kernel.AggCountStar
			spec.Name = "count"
			return spec, nil
		}
		if len(c.Args) != 1 {
			return spec, fmt.Errorf("COUNT expects one argument")
		}
		id, ok := c.Args[0].(*Ident)
		if !ok {
			return spec, fmt.Errorf("COUNT argument must be a column")
		}
		if _, ok := tbl.FieldType(id.Name); !ok {
			return spec, fmt.Errorf("unknown column %q", id.Name)
		}
		spec.Fn = kernel.AggCount
		spec.Input = id.Name
		spec.Name = "count_" + id.Name
		return spec, nil
	case "SUM", "AVG", "MIN", "MAX":
		if c.Star || len(c.Args) != 1 {
			return spec, fmt.Errorf("%s expects one column", name)
		}
		id, ok := c.Args[0].(*Ident)
		if !ok {
			return spec, fmt.Errorf("%s argument must be a column", name)
		}
		typ, ok := tbl.FieldType(id.Name)
		if !ok {
			return spec, fmt.Errorf("unknown column %q", id.Name)
		}
		if (name == "SUM" || name == "AVG") && !isNumeric(typ) {
			return spec, fmt.Errorf("%s requires a numeric column, got %s", name, typ)
		}
		spec.Input = id.Name
		spec.Name = strings.ToLower(name) + "_" + id.Name
		switch name {
		case "SUM":
			spec.Fn = kernel.AggSum
		case "AVG":
			spec.Fn = kernel.AggAvg
		case "MIN":
			spec.Fn = kernel.AggMin
		case "MAX":
			spec.Fn = kernel.AggMax
		}
		return spec, nil
	default:
		return spec, fmt.Errorf("unknown aggregate %s", name)
	}
}

func isNumeric(typ string) bool {
	return strings.Contains(typ, "int") || strings.Contains(typ, "float") || strings.Contains(typ, "double")
}

func checkWhere(e Expr, tbl *catalog.Table) error {
	switch n := e.(type) {
	case *Ident:
		if _, ok := tbl.FieldType(n.Name); !ok {
			return fmt.Errorf("unknown column %q", n.Name)
		}
	case *Literal, *Star:
		return nil
	case *Unary:
		return checkWhere(n.X, tbl)
	case *Binary:
		if n.Op == "+" || n.Op == "-" || n.Op == "*" || n.Op == "/" {
			return fmt.Errorf("arithmetic in WHERE not supported in v1")
		}
		if err := checkWhere(n.Left, tbl); err != nil {
			return err
		}
		return checkWhere(n.Right, tbl)
	case *IsNull:
		return checkWhere(n.X, tbl)
	case *InList:
		if err := checkWhere(n.X, tbl); err != nil {
			return err
		}
		for _, v := range n.Vals {
			if err := checkWhere(v, tbl); err != nil {
				return err
			}
		}
	case *Between:
		if err := checkWhere(n.X, tbl); err != nil {
			return err
		}
		if err := checkWhere(n.Low, tbl); err != nil {
			return err
		}
		return checkWhere(n.High, tbl)
	case *Call:
		return fmt.Errorf("aggregates are not allowed in WHERE")
	}
	return nil
}

func resolveOrder(e Expr, tbl *catalog.Table, alias map[string]string, b *BoundQuery) (string, error) {
	switch n := e.(type) {
	case *Ident:
		if out, ok := alias[n.Name]; ok {
			return out, nil
		}
		for _, p := range b.Project {
			if p == n.Name {
				return p, nil
			}
		}
		if _, ok := tbl.FieldType(n.Name); ok {
			if b.IsAgg {
				for _, g := range b.GroupBy {
					if g == n.Name {
						return n.Name, nil
					}
				}
				return "", fmt.Errorf("ORDER BY %s is not in SELECT/GROUP BY", n.Name)
			}
			return n.Name, nil
		}
		return "", fmt.Errorf("unknown column %q in ORDER BY", n.Name)
	case *Call:
		key := n.String()
		if out, ok := alias[key]; ok {
			return out, nil
		}
		if out, ok := alias[strings.ToUpper(key)]; ok {
			return out, nil
		}
		return "", fmt.Errorf("ORDER BY %s does not match a SELECT aggregate", key)
	default:
		return "", fmt.Errorf("ORDER BY expression not supported in v1")
	}
}

func unionScan(parts ...[]string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, p := range parts {
		for _, s := range p {
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

// Request converts a bound query to an exec pipeline request.
func (b *BoundQuery) Request() exec.Request {
	return exec.Request{
		Table:    b.Table,
		Where:    b.Where,
		ScanCols: b.ScanCols,
		GroupBy:  b.GroupBy,
		Aggs:     b.Aggs,
		Project:  b.Project,
		Order:    b.Order,
		Limit:    b.Limit,
	}
}
