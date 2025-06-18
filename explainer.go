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
		Description: `This view shows where your program is spending its active CPU time.

Analogy: The Photographer
A photographer takes a snapshot of what your CPU is doing 100 times per second. Each snapshot is a "sample". The functions that appear in the most photos are the "hottest" and are using the most CPU.

Use this view to find computationally expensive parts of your code.`,
	},
	"inuse_space": {
		Title: "Heap Profile: In-Use Space",
		Description: `This view shows how much memory is currently being held ("in use") by each function at the moment the profile was taken.

Analogy: The Water Bucket (Current Level)
Imagine a water bucket. This view tells you how much water is IN the bucket RIGHT NOW.

Use this view to find MEMORY LEAKS. If this number constantly grows and never goes down, you are leaking memory.`,
	},
	"alloc_space": {
		Title: "Heap Profile: Allocated Space",
		Description: `This view shows the TOTAL amount of memory allocated by each function over the program's entire life, even if it was later freed.

Analogy: The Water Bucket (Total Water Moved)
Imagine a water bucket. This view tells you the total amount of water that has been POURED THROUGH the bucket all day.

Use this view to find INEFFICIENT code that causes a lot of memory "churn." High allocation puts pressure on the Garbage Collector (GC), which can slow your program down.`,
	},
	"goroutine": {
		Title: "Goroutine Profile",
		Description: `This view shows where all your goroutines (concurrent workers) are currently running or waiting.

Analogy: Office Workers
Your program is an office full of workers (goroutines). This profile is a snapshot of what every single worker is doing at this instant. Are they typing at their desk (running)? Waiting for a coffee (blocked on a channel)? In a meeting (waiting for a lock)?

Use this view to debug concurrency issues, find DEADLOCKS, or discover if your goroutines are all stuck waiting for the same thing.`,
	},
	// Add more for alloc_objects, inuse_objects, mutex, etc. later
}

// getExplanationForView takes a view name like "alloc_space (bytes)" and finds the right help text.
func getExplanationForView(viewName string) Explanation {
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