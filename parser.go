// parser.go
package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/pprof/profile"
)

// FlameNode represents a single function in a flame graph tree.
type FlameNode struct {
	Name     string
	Value    int64
	Children []*FlameNode
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
				if len(loc.Line) == 0 {
					continue
				}
				fun := loc.Line[0].Function
				if _, ok := view.Nodes[fun.ID]; !ok {
					view.Nodes[fun.ID] = &FuncNode{
						ID:        fun.ID,
						Name:      fun.Name,
						FileName:  fun.Filename,
						StartLine: int(loc.Line[0].Line),
						In:        make(map[*FuncNode]int64),
						Out:       make(map[*FuncNode]int64),
					}
				}
				node := view.Nodes[fun.ID]
				node.CumValue += val
				if j == 0 { // Flat value only for the top of the stack
					node.FlatValue += val
				}
			}
		}

		view.TotalValue = totalValueForView

		// Second pass: establish the edges (caller -> callee relationships)
		for _, s := range p.Sample {
			val := s.Value[i]
			if val == 0 {
				continue
			}
			for j := 1; j < len(s.Location); j++ {
				calleeLoc := s.Location[j-1]
				calleeFunc := calleeLoc.Line[0].Function
				calleeNode, ok1 := view.Nodes[calleeFunc.ID]
				callerLoc := s.Location[j]
				callerFunc := callerLoc.Line[0].Function
				callerNode, ok2 := view.Nodes[callerFunc.ID]
				if ok1 && ok2 {
					callerNode.Out[calleeNode] += val
					calleeNode.In[callerNode] += val
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
	// Step 1: Parse both profiles completely.
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

	// Step 2: Iterate through 'after' views to find common views to diff.
	for _, afterView := range afterData.Views {
		baseName := strings.Split(afterView.Name, " ")[0]
		beforeView, ok := beforeViewsMap[baseName]
		if !ok {
			continue
		}

		// Step 3: Create the diff view and its nodes.
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

		// Step 4: Build the delta graph edges.
		type edgeKey struct{ caller, callee uint64 }
		allEdges := make(map[edgeKey]struct{})

		// Collect all edges from 'before' view
		for _, callerNode := range beforeView.Nodes {
			for calleeNode := range callerNode.Out {
				allEdges[edgeKey{caller: callerNode.ID, callee: calleeNode.ID}] = struct{}{}
			}
		}
		// Collect all edges from 'after' view
		for _, callerNode := range afterView.Nodes {
			for calleeNode := range callerNode.Out {
				allEdges[edgeKey{caller: callerNode.ID, callee: calleeNode.ID}] = struct{}{}
			}
		}

		// For each unique edge, calculate the delta.
		for edge := range allEdges {
			var beforeVal, afterVal int64
			if beforeCaller, ok := beforeView.Nodes[edge.caller]; ok {
				if beforeCallee, ok := beforeView.Nodes[edge.callee]; ok {
					// Need to find the specific callee node in the 'Out' map
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
					// Need to find the specific callee node in the 'Out' map
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

func BuildFlameGraph(p *profile.Profile, sampleIndex int) *FlameNode {
	root := &FlameNode{Name: "root", Value: 0}

	for _, s := range p.Sample {
		val := s.Value[sampleIndex]
		if val == 0 {
			continue
		}

		// The call stack is ordered from callee to caller.
		// For a flame graph, we need to reverse it to be caller -> callee.
		currentNode := root
		root.Value += val

		for i := len(s.Location) - 1; i >= 0; i-- {
			loc := s.Location[i]
			if len(loc.Line) == 0 {
				continue
			}
			funcName := loc.Line[0].Function.Name

			// Find if this function is already a child of the current node
			var childNode *FlameNode
			for _, child := range currentNode.Children {
				if child.Name == funcName {
					childNode = child
					break
				}
			}

			// If not found, create a new child node
			if childNode == nil {
				childNode = &FlameNode{Name: funcName}
				currentNode.Children = append(currentNode.Children, childNode)
			}

			childNode.Value += val
			currentNode = childNode // Move down the tree
		}
	}
	return root
}

// AnnotateProjectCode marks nodes that belong to the user's project module.
// The pprof file format often includes the full module path in the function's file name.
// For example: "github.com/your/project/package/file.go".
func annotateProjectCode(data *ProfileData, modulePath string) {
	if data == nil || modulePath == "" {
		return
	}

	// Make sure the path has a trailing slash for more accurate matching
	// to avoid matching "github.com/user/project-extra" if module is "github.com/user/project"
	normalizedPath := modulePath
	if !strings.HasSuffix(normalizedPath, "/") {
		normalizedPath += "/"
	}

	for _, view := range data.Views {
		for _, node := range view.Nodes {
			// We use strings.Contains because filenames can be absolute paths.
			// e.g., /Users/me/go/src/github.com/my/project/main.go
			if strings.Contains(node.FileName, normalizedPath) {
				node.IsProjectCode = true
			}
		}
	}
}
