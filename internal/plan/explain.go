package plan

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/kernel"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/parquetscan"
)

// Text is the human-readable EXPLAIN tree.
func (n *Node) Text() string {
	var b strings.Builder
	n.writeText(&b, 0)
	return strings.TrimRight(b.String(), "\n")
}

func (n *Node) writeText(b *strings.Builder, indent int) {
	if n == nil {
		return
	}
	pad := strings.Repeat("  ", indent)
	switch n.Op {
	case OpLimit:
		fmt.Fprintf(b, "%sLimit %d\n", pad, n.Limit)
	case OpSort:
		parts := make([]string, len(n.Order))
		for i, k := range n.Order {
			dir := "ASC"
			if k.Desc {
				dir = "DESC"
			}
			parts[i] = k.Name + " " + dir
		}
		fmt.Fprintf(b, "%sSort %s\n", pad, strings.Join(parts, ", "))
	case OpAgg:
		keys := strings.Join(n.GroupBy, ", ")
		if keys == "" {
			keys = "(scalar)"
		}
		aggs := make([]string, len(n.Aggs))
		for i, a := range n.Aggs {
			aggs[i] = aggString(a)
		}
		fmt.Fprintf(b, "%sHashAggregate keys=[%s] aggs=[%s]\n", pad, keys, strings.Join(aggs, ", "))
	case OpProject:
		fmt.Fprintf(b, "%sProject columns=[%s]\n", pad, strings.Join(n.Project, ", "))
	case OpFilter:
		res := "(none)"
		if n.Residual != nil {
			res = n.Residual.String()
		}
		fmt.Fprintf(b, "%sFilter residual=%s\n", pad, res)
	case OpEmpty:
		fmt.Fprintf(b, "%sEmpty (constant false / all row groups skipped)\n", pad)
	case OpScan:
		fmt.Fprintf(b, "%sParquetScan table=%s\n", pad, n.Table)
		fmt.Fprintf(b, "%s  files=%d  row_groups=%d  kept_row_groups=%d  skipped=%d\n",
			pad, n.Files, n.RowGroupsTotal, n.RowGroupsKept, n.RowGroupsSkipped)
		cols := n.Keep
		if len(cols) == 0 {
			cols = []string{"*"}
		}
		prune := ""
		if n.PrunedCols > 0 {
			prune = fmt.Sprintf("   -- pruned %d cols", n.PrunedCols)
		}
		fmt.Fprintf(b, "%s  columns=[%s]%s\n", pad, strings.Join(cols, ", "), prune)
		pushed := "(none)"
		if n.Pushed != nil {
			pushed = n.Pushed.String()
		}
		fmt.Fprintf(b, "%s  pushed=%s\n", pad, pushed)
		if n.Limit > 0 {
			fmt.Fprintf(b, "%s  limit_pushdown=%d\n", pad, n.Limit)
		}
		if n.Jobs > 1 {
			fmt.Fprintf(b, "%s  jobs=%d\n", pad, n.Jobs)
		}
		if n.Analyze {
			fmt.Fprintf(b, "%s  bytes_read=%s  rows_in=%d\n", pad, parquetscan.FormatBytes(n.BytesRead), n.RowsRead)
		}
	default:
		fmt.Fprintf(b, "%s%s\n", pad, n.Op)
	}
	if n.Child != nil {
		n.Child.writeText(b, indent+1)
	}
}

func aggString(a kernel.AggSpec) string {
	switch a.Fn {
	case kernel.AggCountStar:
		return "count(*)"
	case kernel.AggCount:
		return "count(" + a.Input + ")"
	default:
		return a.Fn.String() + "(" + a.Input + ")"
	}
}

// JSONNode is the UI-friendly EXPLAIN encoding.
type JSONNode struct {
	Op               string    `json:"op"`
	Table            string    `json:"table,omitempty"`
	Columns          []string  `json:"columns,omitempty"`
	Pushed           string    `json:"pushed,omitempty"`
	Residual         string    `json:"residual,omitempty"`
	GroupBy          []string  `json:"group_by,omitempty"`
	Aggs             []string  `json:"aggs,omitempty"`
	Order            []string  `json:"order,omitempty"`
	Limit            int64     `json:"limit,omitempty"`
	Files            int       `json:"files,omitempty"`
	RowGroups        int       `json:"row_groups,omitempty"`
	KeptRowGroups    int       `json:"kept_row_groups,omitempty"`
	SkippedRowGroups int       `json:"skipped_row_groups,omitempty"`
	PrunedCols       int       `json:"pruned_cols,omitempty"`
	BytesRead        int64     `json:"bytes_read,omitempty"`
	RowsIn           int64     `json:"rows_in,omitempty"`
	Jobs             int       `json:"jobs,omitempty"`
	Child            *JSONNode `json:"child,omitempty"`
}

func (n *Node) JSON() *JSONNode {
	if n == nil {
		return nil
	}
	j := &JSONNode{Op: n.Op, Table: n.Table, Limit: n.Limit, GroupBy: n.GroupBy}
	j.Columns = n.Keep
	if n.Pushed != nil {
		j.Pushed = n.Pushed.String()
	}
	if n.Residual != nil {
		j.Residual = n.Residual.String()
	}
	for _, a := range n.Aggs {
		j.Aggs = append(j.Aggs, aggString(a))
	}
	for _, k := range n.Order {
		s := k.Name
		if k.Desc {
			s += " DESC"
		} else {
			s += " ASC"
		}
		j.Order = append(j.Order, s)
	}
	if n.Op == OpScan {
		j.Files = n.Files
		j.RowGroups = n.RowGroupsTotal
		j.KeptRowGroups = n.RowGroupsKept
		j.SkippedRowGroups = n.RowGroupsSkipped
		j.PrunedCols = n.PrunedCols
		j.Jobs = n.Jobs
		if n.Analyze {
			j.BytesRead = n.BytesRead
			j.RowsIn = n.RowsRead
		}
	}
	if n.Op == OpProject {
		j.Columns = n.Project
	}
	j.Child = n.Child.JSON()
	return j
}

// AttachStats fills EXPLAIN ANALYZE counters on the scan node.
func (n *Node) AttachStats(st parquetscan.Stats) {
	if n == nil {
		return
	}
	n.Analyze = true
	if scan := n.find(OpScan); scan != nil {
		scan.Analyze = true
		scan.BytesRead = st.CompressedBytes
		scan.RowsRead = st.RowsRead
		if st.RowGroupsTotal > 0 {
			scan.RowGroupsTotal = st.RowGroupsTotal
			scan.RowGroupsKept = st.RowGroupsRead
			scan.RowGroupsSkipped = st.RowGroupsSkipped
		}
	}
}

func (n *Node) JSONString() (string, error) {
	b, err := json.MarshalIndent(n.JSON(), "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
