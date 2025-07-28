// explainer.go
package main

import "strings"

// Explanation holds the title and text for a help topic.
type Explanation struct {
	Title       string
	Description string
}

// explainerMap contains our human-friendly explanations for pprof terms.
var explainerMap = map[string]Explanation{
	"cpu": {
		Title: "CPU Profile (cpu, samples)",
		Description: `This view shows where your program is spending active CPU time — not wall clock time.

What are "samples"?
During profiling, the Go runtime pauses your program about 100 times per second (default). Each pause records which function was running at that exact moment. These are called "samples".

If a function appears in many samples, it means the CPU spent a lot of time executing that function.

Use this view to identify CPU hotspots — the most computationally expensive parts of your code.

Note: This profile does not include time spent sleeping, waiting on I/O, channels, or locks.`,
	},

	"inuse_space": {
		Title: "Heap Profile: In-Use Space (Bytes)",
		Description: `This view shows how much memory is currently being held in memory ("in use") by each function at the time the profile was captured.

Analogy: Imagine checking how much water is in a bucket right now.

What does "grows over time" mean?
If you collect multiple profiles over time (e.g., every 30 seconds), and the in-use memory keeps increasing without going back down — even when the workload stays the same — it may indicate a memory leak.

Use this view to detect memory leaks by watching for memory usage that trends upward over time without releasing memory.`,
	},

	"alloc_space": {
		Title: "Heap Profile: Allocated Space (Bytes)",
		Description: `This view shows the total amount of memory allocated by each function over the program's lifetime — even if it was later garbage collected.

Analogy: This is like measuring the total amount of water that has flowed through a bucket during the day.

What is "memory churn"?
Functions that allocate a lot of temporary data (e.g., slices, strings, structs in loops) may cause frequent allocations and garbage collections. This puts pressure on the GC and can slow down your program.

Use this view to find inefficient code that causes excessive allocation activity.`,
	},

	"inuse_objects": {
		Title: "Heap Profile: In-Use Objects",
		Description: `This view shows how many individual objects are currently held in memory — not the total size, but the object count.

Use this when you're investigating cases where many small objects are being retained, such as structs, buffers, or interface values.

Helpful for diagnosing object leaks or high object retention even when overall memory usage seems stable.`,
	},

	"alloc_objects": {
		Title: "Heap Profile: Allocated Objects",
		Description: `This view shows the total number of objects allocated over the life of the program, including those that were later freed.

Use this to understand allocation patterns. A high number of allocations may point to inefficient object creation in tight loops or repeated calls.

Useful for spotting places where you can reduce object churn.`,
	},

	"goroutine": {
		Title: "Goroutine Profile",
		Description: `This view shows where all goroutines in your program are currently running or waiting.

Analogy: Think of your program as an office of workers. This view shows what each worker (goroutine) is doing:
- Actively working = Running
- Waiting in line = Blocked on a channel or mutex
- On hold = Waiting for I/O or another task

Use this view to diagnose concurrency issues such as goroutines stuck waiting, deadlocks, or inefficient scheduling.`,
	},

	"mutex": {
		Title: "Mutex Contention Profile",
		Description: `This view shows where goroutines are blocked while waiting to acquire a mutex (lock).

Analogy: If many workers are stuck waiting for the same locked door, you’ll see a build-up in this profile.

Use this view to detect lock contention and pinpoint code that causes bottlenecks in concurrent access.`,
	},

	"flat_vs_cum": {
		Title: "Flat vs. Cumulative (Cum) - Understanding Percentages",
		Description: `'Flat' is what this function alone consumed.  
'Cumulative' (Cum) is this function + everything it called.

The percentages show:
- Flat 40% = "Out of 100% total CPU/memory sampled, this function alone used 40%"
- Cum 60% = "This function + all its callees together used 60% of the total"

Analogy: A manager delegates a task.  
- Flat % = Time the manager spends giving instructions  
- Cum % = Total time for the entire task (manager + all workers)

Use this to distinguish whether a function is expensive on its own or calling other expensive functions.`,
	},

	"flamegraph": {
		Title: "Flame Graph (Icicle View)",
		Description: `This view shows your program's call stack as a visual chart. In icicle view, the root functions are at the top and their callees stack downward.

How to read it:
- Horizontal width of each block shows how much time (CPU) or memory was spent in that function and its callees.
- Vertical depth shows call stack nesting — a deeper stack means more function calls.

Functions near the bottom with wide blocks are usually doing the most work.

This layout is known as an "icicle graph", and it's now common in tools like pprof, speedscope, and browser devtools.

Use this to quickly understand which code paths consume the most resources and how they are nested.`,
	},

	"diff": {
		Title: "Profile Diff Comparison",
		Description: `You're comparing two profiles to see what changed between them.

How to read the results:
- Green (+) = More time/memory used in the second profile (regression)
- Red (-) = Less time/memory used in the second profile (improvement)

Impact levels:
- "major impact" = Changes affecting >5% of total time/memory
- Regular changes = 1-5% impact
- "minor" = <1% impact (often just noise)

Special indicators:
- "introduced" = New functions that appeared
- "eliminated" = Functions that disappeared
- "2.0x faster/slower" = Performance ratio changes

Tips:
- Focus on "major impact" items first
- Press 'p' to show only your project code
- Look for patterns: did one slow function get replaced by a faster one?`,
	},
}

// getExplanationForView takes a view name like "alloc_space (bytes)" and finds the right help text.
func getExplanationForView(viewName string) Explanation {
	// Check for diff mode first
	if strings.HasPrefix(viewName, "Diff:") {
		return explainerMap["diff"]
	}

	// Look for a keyword in the view name
	if strings.Contains(viewName, "cpu") || strings.Contains(viewName, "samples") {
		return explainerMap["cpu"]
	}
	if strings.Contains(viewName, "inuse_space") {
		return explainerMap["inuse_space"]
	}
	if strings.Contains(viewName, "alloc_space") {
		return explainerMap["alloc_space"]
	}
	if strings.Contains(viewName, "goroutine") {
		return explainerMap["goroutine"]
	}
	// Default explanation
	return Explanation{
		Title:       viewName,
		Description: "No specific explanation available for this profile type yet.",
	}
}
