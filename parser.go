// parser.go
package main

import (
	"fmt"
	"io"
	"sort"
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

// ProfileView represents a single way to look at the data (e.g., CPU time, In-use space).
type ProfileView struct {
	Name      string
	Functions []FunctionProfile
}

// ProfileData holds all the parsed views from a single pprof file.
type ProfileData struct {
	Views []*ProfileView
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

// ParsePprofFile reads an io.Reader and returns all available profile views.
func ParsePprofFile(reader io.Reader) (*ProfileData, error) {
	p, err := profile.Parse(reader)
	if err != nil {
		return nil, fmt.Errorf("could not parse pprof data: %w", err)
	}

	profileData := &ProfileData{}

	// p.SampleType describes what each value in a sample means.
	// For heap profiles, this will have multiple entries (inuse_space, alloc_objects, etc.).
	for i, sampleType := range p.SampleType {
		view := &ProfileView{
			Name: fmt.Sprintf("%s (%s)", sampleType.Type, sampleType.Unit),
		}

		funcMap := make(map[uint64]*FunctionProfile)

		for _, s := range p.Sample {
			// Get the value for the *current* sample type we are processing.
			val := s.Value[i]
			if val == 0 {
				continue // Skip samples that don't have a value for this type.
			}

			if len(s.Location) > 0 {
				loc := s.Location[0]
				if len(loc.Line) > 0 {
					line := loc.Line[0]
					fun := line.Function

					if _, ok := funcMap[fun.ID]; !ok {
						funcMap[fun.ID] = &FunctionProfile{
							Name:      fun.Name,
							FileName:  fun.Filename,
							StartLine: int(line.Line),
						}
					}
					funcMap[fun.ID].FlatValue += val
				}
			}
		}

		for _, f := range funcMap {
			view.Functions = append(view.Functions, *f)
		}

		sort.Slice(view.Functions, func(j, k int) bool {
			return view.Functions[j].FlatValue > view.Functions[k].FlatValue
		})

		profileData.Views = append(profileData.Views, view)
	}

	if len(profileData.Views) == 0 {
		return nil, fmt.Errorf("no valid sample data found in profile")
	}

	return profileData, nil
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
