// parser.go
package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/google/pprof/profile"
)

// FunctionProfile holds the processed data for a single function.
type FunctionProfile struct {
	Name      string
	FileName  string
	StartLine int
	FlatValue int64 // The 'self' cost of the function
}

// ParseProfile now takes an io.Reader and correctly calculates Flat time.
func ParseProfile(reader io.Reader) ([]FunctionProfile, error) {
	p, err := profile.Parse(reader)
	if err != nil {
		return nil, fmt.Errorf("could not parse pprof data: %w", err)
	}

	// This map will aggregate flat values for each function.
	funcMap := make(map[uint64]*FunctionProfile)

	// --- THIS IS THE CORRECTED LOGIC ---
	for _, s := range p.Sample {
		// A sample has a stack of locations. The first location (index 0)
		// is the function that was actively running on the CPU at the time of the sample.
		// This is the only location that contributes to "Flat" time.
		if len(s.Location) > 0 {
			location := s.Location[0] // Key change: Only look at the top of the stack.

			// A single location can have multiple lines due to inlining.
			// We attribute the cost to the most specific function in that line.
			if len(location.Line) > 0 {
				line := location.Line[0]
				function := line.Function

				// If we haven't seen this function before, create an entry for it.
				if _, ok := funcMap[function.ID]; !ok {
					funcMap[function.ID] = &FunctionProfile{
						Name:      function.Name,
						FileName:  function.Filename,
						StartLine: int(line.Line),
					}
				}
				// Add the sample's value to this function's flat time.
				// We assume the first value is the one we care about (e.g., cpu-time).
				funcMap[function.ID].FlatValue += s.Value[0]
			}
		}
	}

	// Convert the map to a slice for sorting.
	var functions []FunctionProfile
	for _, f := range funcMap {
		functions = append(functions, *f)
	}

	// Sort functions by flat value, descending.
	sort.Slice(functions, func(i, j int) bool {
		return functions[i].FlatValue > functions[j].FlatValue
	})

	return functions, nil
}