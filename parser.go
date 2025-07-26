// parser.go
package main

import (
	"fmt"
	"hash/fnv"
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

// ChangeType represents the type of change for a function in diff mode
type ChangeType int

const (
	Modified ChangeType = iota
	New
	Removed
)

// FuncNode represents a single function in our call graph.
type FuncNode struct {
	ID        uint64
	Name      string
	FileName  string
	StartLine int
	FlatValue int64
	CumValue  int64 // Cumulative value (this function + children)

	FlatDelta  int64
	CumDelta   int64
	FlatRatio  float64
	CumRatio   float64
	ChangeType ChangeType

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

		// First pass: create all function nodes and calculate cumulative values.
		// This part must also handle inlining correctly.
		for _, s := range p.Sample {
			val := s.Value[i]
			if val == 0 {
				continue
			}
			totalValueForView += val

			for j, loc := range s.Location {
				for _, line := range loc.Line {
					fun := line.Function
					if _, ok := view.Nodes[fun.ID]; !ok {
						view.Nodes[fun.ID] = &FuncNode{
							ID:        fun.ID,
							Name:      fun.Name,
							FileName:  fun.Filename,
							StartLine: int(line.Line),
							In:        make(map[*FuncNode]int64),
							Out:       make(map[*FuncNode]int64),
						}
					}
					node := view.Nodes[fun.ID]
					node.CumValue += val
				}
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
		// This must correctly handle both regular calls and inlined calls.
		for _, s := range p.Sample {
			val := s.Value[i]
			if val == 0 {
				continue
			}

			// Unroll the entire stack for this sample into a single, flat list of functions.
			var callchain []*FuncNode
			for j := len(s.Location) - 1; j >= 0; j-- { // From caller to callee
				loc := s.Location[j]
				// Unroll inlined functions within the location. Proto spec says last line is the caller.
				for k := len(loc.Line) - 1; k >= 0; k-- {
					line := loc.Line[k]
					if node, ok := view.Nodes[line.Function.ID]; ok {
						callchain = append(callchain, node)
					}
				}
			}

			// Now, create edges between adjacent functions in the fully unrolled chain.
			for j := 0; j < len(callchain)-1; j++ {
				callerNode := callchain[j]
				calleeNode := callchain[j+1]

				// Add the value to the edge weight. Use a map to avoid double counting edges.
				callerNode.Out[calleeNode] += val
				calleeNode.In[callerNode] += val
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

// hashString creates a stable uint64 hash from a string
func hashString(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

func DiffPprofFiles(beforeReader, afterReader io.Reader) (*ProfileData, error) {
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

		// Create function signature to node mapping for stable matching
		// Function signature: "name|filename|startline"
		beforeFuncMap := make(map[string]*FuncNode)
		for _, node := range beforeView.Nodes {
			sig := fmt.Sprintf("%s|%s|%d", node.Name, node.FileName, node.StartLine)
			beforeFuncMap[sig] = node
		}

		afterFuncMap := make(map[string]*FuncNode)
		for _, node := range afterView.Nodes {
			sig := fmt.Sprintf("%s|%s|%d", node.Name, node.FileName, node.StartLine)
			afterFuncMap[sig] = node
		}

		// Get all unique function signatures
		allFuncSigs := make(map[string]struct{})
		for sig := range beforeFuncMap {
			allFuncSigs[sig] = struct{}{}
		}
		for sig := range afterFuncMap {
			allFuncSigs[sig] = struct{}{}
		}

		for sig := range allFuncSigs {
			beforeNode, hasBefore := beforeFuncMap[sig]
			afterNode, hasAfter := afterFuncMap[sig]

			var baseNode *FuncNode
			if hasAfter {
				baseNode = afterNode
			} else {
				baseNode = beforeNode
			}

			// Generate a stable ID based on function signature
			// This ensures the same function gets the same ID across different profiles
			stableID := hashString(sig)

			diffNode := &FuncNode{
				ID:        stableID,
				Name:      baseNode.Name,
				FileName:  baseNode.FileName,
				StartLine: baseNode.StartLine,
				In:        make(map[*FuncNode]int64),
				Out:       make(map[*FuncNode]int64),
			}

			// Calculate before and after values for ratio calculation
			var beforeFlat, afterFlat, beforeCum, afterCum int64
			if hasBefore {
				beforeFlat = beforeNode.FlatValue
				beforeCum = beforeNode.CumValue
				diffNode.FlatDelta -= beforeNode.FlatValue
				diffNode.CumDelta -= beforeNode.CumValue
			}
			if hasAfter {
				afterFlat = afterNode.FlatValue
				afterCum = afterNode.CumValue
				diffNode.FlatDelta += afterNode.FlatValue
				diffNode.CumDelta += afterNode.CumValue
			}

			// Calculate ratios
			diffNode.FlatRatio = calculateRatio(beforeFlat, afterFlat)
			diffNode.CumRatio = calculateRatio(beforeCum, afterCum)

			// Determine change type
			if !hasBefore && hasAfter {
				diffNode.ChangeType = New
			} else if hasBefore && !hasAfter {
				diffNode.ChangeType = Removed
			} else {
				diffNode.ChangeType = Modified
			}

			diffView.Nodes[stableID] = diffNode
		}
		// TODO: Fix edge processing to work with signature-based matching

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
