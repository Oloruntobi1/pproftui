// parser.go
package main

import (
	"fmt"
	"io"
	"time"

	"github.com/google/pprof/profile"
)

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

	// Graph structure
	In  map[*FuncNode]int64 // Callers: map[caller]edge_weight
	Out map[*FuncNode]int64 // Callees: map[callee]edge_weight
}

// ProfileView now contains a graph of nodes.
type ProfileView struct {
	Name  string
	Unit  string
	Nodes map[uint64]*FuncNode // All nodes in this view, indexed by function ID
}

// ProfileData holds all the parsed views from a single pprof file.
type ProfileData struct {
	Views []*ProfileView
}

// ParsePprofFile builds a full call graph for each profile type.
// parser.go

func ParsePprofFile(reader io.Reader) (*ProfileData, error) {
	p, err := profile.Parse(reader)
	if err != nil {
		return nil, fmt.Errorf("could not parse pprof data: %w", err)
	}

	profileData := &ProfileData{}

	for i, sampleType := range p.SampleType {
		view := &ProfileView{
			Name:  fmt.Sprintf("%s (%s)", sampleType.Type, sampleType.Unit),
			Unit:  sampleType.Unit,
			Nodes: make(map[uint64]*FuncNode),
		}

		// First pass: create all function nodes and calculate flat/cum values.
		// This ensures all nodes exist before we start creating edges.
		for _, s := range p.Sample {
			val := s.Value[i]
			if val == 0 {
				continue
			}
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

		// Second pass: establish the edges (caller -> callee relationships)
		for _, s := range p.Sample {
			val := s.Value[i]
			if val == 0 {
				continue
			}
			// The call stack is ordered callee -> caller -> caller's caller ...
			// So for any j > 0, location[j] is the caller of location[j-1].
			for j := 1; j < len(s.Location); j++ {
				// The function that was called (the callee)
				calleeLoc := s.Location[j-1]
				calleeFunc := calleeLoc.Line[0].Function
				calleeNode := view.Nodes[calleeFunc.ID]

				// The function that made the call (the caller)
				callerLoc := s.Location[j]
				callerFunc := callerLoc.Line[0].Function
				callerNode := view.Nodes[callerFunc.ID]

				// Establish the link if both nodes exist
				if callerNode != nil && calleeNode != nil {
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
