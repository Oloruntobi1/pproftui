package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/pprof/profile"
)

// FlameNode represents a single function in a flame graph tree.
type FlameNode struct {
	Name     string
	Value    int64
	Children []*FlameNode
	Parent   *FlameNode // Parent pointer for easier traversal (zoom, breadcrumbs)
}

// FunctionProfile holds the raw data for a function.
type FunctionProfile struct {
	Name      string
	FileName  string
	StartLine int
	FlatValue int64
}

// FuncNode represents a single function in our call graph.
type FuncNode struct {
	ID        uint64
	Name      string
	FileName  string
	StartLine int
	FlatValue int64
	CumValue  int64 // Cumulative value (this function + children)

	FlatDelta int64
	CumDelta  int64

	IsProjectCode bool

	// Graph structure
	In  map[*FuncNode]int64 // Callers: map[caller]edge_weight
	Out map[*FuncNode]int64 // Callees: map[callee]edge_weight
}

type ProfileView struct {
	Name       string
	Unit       string
	TotalValue int64                // The sum of all samples in this view.
	Nodes      map[uint64]*FuncNode // All nodes in this view, indexed by function ID
}

// ProfileData holds all the parsed views from a single pprof file.
type ProfileData struct {
	DurationNanos int64
	Views         []*ProfileView
	RawPprof      *profile.Profile
}

// ParsePprofFile builds a full call graph for each profile type.
func ParsePprofFile(reader io.Reader) (*ProfileData, error) {
	p, err := profile.Parse(reader)
	if err != nil {
		return nil, fmt.Errorf("could not parse pprof data: %w", err)
	}

	profileData := &ProfileData{
		RawPprof:      p,
		DurationNanos: p.DurationNanos,
	}

	for i, sampleType := range p.SampleType {
		view := &ProfileView{
			Name:  fmt.Sprintf("%s (%s)", sampleType.Type, sampleType.Unit),
			Unit:  sampleType.Unit,
			Nodes: make(map[uint64]*FuncNode),
		}

		var totalValueForView int64

		// First pass: create all function nodes and calculate flat/cum values.
		for _, s := range p.Sample {
			val := s.Value[i]
			if val == 0 {
				continue
			}
			totalValueForView += val

			for j, loc := range s.Location {
				// To calculate cumulative value correctly, we must consider all
				// functions in the inlined chain.
				for _, line := range loc.Line {
					fun := line.Function
					if _, ok := view.Nodes[fun.ID]; !ok {
						view.Nodes[fun.ID] = &FuncNode{
							ID:        fun.ID,
							Name:      fun.Name,
							FileName:  fun.Filename,
							StartLine: int(line.Line), // Use the line number from the Line object
							In:        make(map[*FuncNode]int64),
							Out:       make(map[*FuncNode]int64),
						}
					}
					node := view.Nodes[fun.ID]
					node.CumValue += val
				}
				// Flat value still only applies to the top of the stack (leaf-most function)
				if j == 0 && len(loc.Line) > 0 {
					fun := loc.Line[0].Function
					if node, ok := view.Nodes[fun.ID]; ok {
						node.FlatValue += val
					}
				}
			}
		}

		view.TotalValue = totalValueForView

		// Second pass: establish the edges (caller -> callee relationships)
		// This part becomes more complex with inlining.
		for _, s := range p.Sample {
			val := s.Value[i]
			if val == 0 {
				continue
			}
			for j := 1; j < len(s.Location); j++ {
				// The callee is the leaf of the previous location's inlined chain.
				calleeLoc := s.Location[j-1]
				// The caller is the leaf of this location's inlined chain.
				callerLoc := s.Location[j]

				if len(calleeLoc.Line) > 0 && len(callerLoc.Line) > 0 {
					calleeFunc := calleeLoc.Line[0].Function
					callerFunc := callerLoc.Line[0].Function

					calleeNode, ok1 := view.Nodes[calleeFunc.ID]
					callerNode, ok2 := view.Nodes[callerFunc.ID]
					if ok1 && ok2 {
						callerNode.Out[calleeNode] += val
						calleeNode.In[callerNode] += val
					}
				}
			}
		}
		profileData.Views = append(profileData.Views, view)
	}

	if len(profileData.Views) == 0 {
		return nil, fmt.Errorf("no valid sample data found in profile")
	}

	return profileData, nil
}

// formatValue intelligently formats a value based on its unit.
func formatValue(value int64, unit string) string {
	switch unit {
	case "nanoseconds":
		return fmt.Sprintf("%v", formatNanos(value))
	case "bytes":
		return fmt.Sprintf("%v", formatBytes(value))
	default: // "count", "objects", etc.
		return fmt.Sprintf("%d", value)
	}
}

// formatBytes converts bytes to a human-readable string (KB, MB, GB).
func formatBytes(b int64) string {
	if b == 0 {
		return "0 B"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatNanos(n int64) string {
	if n == 0 {
		return "0s"
	}
	d := time.Duration(n)
	return d.String()
}

func DiffPprofFiles(beforeReader, afterReader io.Reader) (*ProfileData, error) {
	// ... (This function remains the same, no changes needed)
	beforeData, err := ParsePprofFile(beforeReader)
	if err != nil {
		return nil, fmt.Errorf("could not parse 'before' profile: %w", err)
	}
	afterData, err := ParsePprofFile(afterReader)
	if err != nil {
		return nil, fmt.Errorf("could not parse 'after' profile: %w", err)
	}

	beforeViewsMap := make(map[string]*ProfileView)
	for _, v := range beforeData.Views {
		baseName := strings.Split(v.Name, " ")[0]
		beforeViewsMap[baseName] = v
	}

	diffProfileData := &ProfileData{
		DurationNanos: afterData.DurationNanos,
		RawPprof:      afterData.RawPprof,
	}

	for _, afterView := range afterData.Views {
		baseName := strings.Split(afterView.Name, " ")[0]
		beforeView, ok := beforeViewsMap[baseName]
		if !ok {
			continue
		}

		diffView := &ProfileView{
			Name:       fmt.Sprintf("Diff: %s", strings.TrimPrefix(afterView.Name, "Diff: ")),
			Unit:       afterView.Unit,
			Nodes:      make(map[uint64]*FuncNode),
			TotalValue: afterView.TotalValue - beforeView.TotalValue,
		}

		allNodeIDs := make(map[uint64]struct{})
		for id := range beforeView.Nodes {
			allNodeIDs[id] = struct{}{}
		}
		for id := range afterView.Nodes {
			allNodeIDs[id] = struct{}{}
		}

		for id := range allNodeIDs {
			beforeNode, hasBefore := beforeView.Nodes[id]
			afterNode, hasAfter := afterView.Nodes[id]

			var baseNode *FuncNode
			if hasAfter {
				baseNode = afterNode
			} else {
				baseNode = beforeNode
			}

			diffNode := &FuncNode{
				ID:        baseNode.ID,
				Name:      baseNode.Name,
				FileName:  baseNode.FileName,
				StartLine: baseNode.StartLine,
				In:        make(map[*FuncNode]int64),
				Out:       make(map[*FuncNode]int64),
			}

			if hasBefore {
				diffNode.FlatDelta -= beforeNode.FlatValue
				diffNode.CumDelta -= beforeNode.CumValue
			}
			if hasAfter {
				diffNode.FlatDelta += afterNode.FlatValue
				diffNode.CumDelta += afterNode.CumValue
			}
			diffView.Nodes[id] = diffNode
		}
		type edgeKey struct{ caller, callee uint64 }
		allEdges := make(map[edgeKey]struct{})
		for _, callerNode := range beforeView.Nodes {
			for calleeNode := range callerNode.Out {
				allEdges[edgeKey{caller: callerNode.ID, callee: calleeNode.ID}] = struct{}{}
			}
		}
		for _, callerNode := range afterView.Nodes {
			for calleeNode := range callerNode.Out {
				allEdges[edgeKey{caller: callerNode.ID, callee: calleeNode.ID}] = struct{}{}
			}
		}
		for edge := range allEdges {
			var beforeVal, afterVal int64
			if beforeCaller, ok := beforeView.Nodes[edge.caller]; ok {
				if beforeCallee, ok := beforeView.Nodes[edge.callee]; ok {
					for node, val := range beforeCaller.Out {
						if node.ID == beforeCallee.ID {
							beforeVal = val
							break
						}
					}
				}
			}
			if afterCaller, ok := afterView.Nodes[edge.caller]; ok {
				if afterCallee, ok := afterView.Nodes[edge.callee]; ok {
					for node, val := range afterCaller.Out {
						if node.ID == afterCallee.ID {
							afterVal = val
							break
						}
					}
				}
			}
			edgeDelta := afterVal - beforeVal
			if edgeDelta == 0 {
				continue
			}

			diffCaller := diffView.Nodes[edge.caller]
			diffCallee := diffView.Nodes[edge.callee]
			diffCaller.Out[diffCallee] = edgeDelta
			diffCallee.In[diffCaller] = edgeDelta
		}

		diffProfileData.Views = append(diffProfileData.Views, diffView)
	}

	if len(diffProfileData.Views) == 0 {
		return nil, fmt.Errorf("no common profile types found to diff between the two files")
	}

	return diffProfileData, nil
}

// BuildFlameGraph constructs a full, cumulative flame graph tree, correctly
// handling inlined function calls.
func BuildFlameGraph(p *profile.Profile, sampleIndex int, unit string) *FlameNode {
	root := &FlameNode{Name: "root"}
	if p == nil || len(p.Sample) == 0 || sampleIndex >= len(p.SampleType) {
		return root
	}

	var totalValue int64

	for _, s := range p.Sample {
		val := s.Value[sampleIndex]
		if val == 0 {
			continue
		}
		totalValue += val

		// Start with the root of our flame graph tree for this sample.
		currentNode := root

		// Iterate through the locations in the stack, from caller to callee.
		for i := len(s.Location) - 1; i >= 0; i-- {
			loc := s.Location[i]

			// Unroll the inlined functions within this location.
			// The proto spec says the last line is the caller and previous lines
			// were inlined into it. So we iterate backward through the lines too.
			for j := len(loc.Line) - 1; j >= 0; j-- {
				line := loc.Line[j]
				funcName := line.Function.Name

				var childNode *FlameNode
				for _, child := range currentNode.Children {
					if child.Name == funcName {
						childNode = child
						break
					}
				}

				if childNode == nil {
					childNode = &FlameNode{Name: funcName, Parent: currentNode}
					currentNode.Children = append(currentNode.Children, childNode)
				}

				// The value applies to this function and all its callers.
				childNode.Value += val

				// Descend into this frame. The next frame (either from the next
				// inlined function or the next location) will be its child.
				currentNode = childNode
			}
		}
	}

	root.Value = totalValue

	sortChildren(root)
	return root
}

// sortChildren recursively sorts children of a node by value (desc) for a stable layout.
func sortChildren(node *FlameNode) {
	if node == nil || len(node.Children) == 0 {
		return
	}
	sort.Slice(node.Children, func(i, j int) bool {
		return node.Children[i].Value > node.Children[j].Value
	})
	for _, child := range node.Children {
		sortChildren(child)
	}
}

// AnnotateProjectCode marks nodes that belong to the user's project module.
func annotateProjectCode(data *ProfileData, modulePath string) {
	if data == nil || modulePath == "" {
		return
	}

	normalizedPath := modulePath
	if !strings.HasSuffix(normalizedPath, "/") {
		normalizedPath += "/"
	}

	for _, view := range data.Views {
		for _, node := range view.Nodes {
			if strings.Contains(node.FileName, normalizedPath) {
				node.IsProjectCode = true
			}
		}
	}
}
