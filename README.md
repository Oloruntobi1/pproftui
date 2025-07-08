# pproftui: Interactive Go Profiling in Your Terminal

`pproftui` is a terminal-based UI for Go's `pprof` that makes profiling interactive, intuitive, and fast. It's designed to help you pinpoint performance issues without the context-switching of a web browser and to provide built-in explanations for profiling concepts.

![Demo](./demo.gif)

---

## Why `pproftui`?

The standard `go tool pprof` is powerful but can be unintuitive. You're often left wondering:
*   "What does `flat` vs `cum` actually mean?"
*   "Which of these 100 runtime functions is *my* code?"
*   "How do I find what changed between this version and the last?"
*   "Why do I have to keep switching back and forth between my browser and my code?"

`pproftui` solves these problems directly:
*   **Integrated Source & Graph Views:** See the source code for any function or navigate its call graph without ever leaving your terminal.
*   **Focus on Your Code:** Automatically highlights functions from your project. Press `p` to instantly hide all the runtime and vendor noise.
*   **Powerful, Intuitive Diffing:** Compare two profiles to see not just *what* changed, but *why*. The UI helps you trace the source of a regression through the call stack.
*   **Live Profiling:** Point `pproftui` at a running service and watch its profile update in real-time.
*   **Built-in Help (F1):** Get clear, simple explanations of profiling terms (`cpu`, `inuse_space`, `flat` vs `cum`) right when you need them.

---

## Getting Started

### Installation
```sh
go install github.com/Oloruntobi1/pproftui@latest
```

### The Cookbook: Common Profiling Workflows

Here are practical recipes for solving common performance problems with `pproftui`.

#### Recipe 1: Investigating a Slowdown in a Local Binary
You've noticed a function is slow. Let's get a CPU profile and explore it.

1.  **Generate the profile:**
    ```sh
    go test -bench=. -cpuprofile=cpu.prof
    ```

2.  **Analyze it with `pproftui`:**
    ```sh
    pproftui cpu.prof
    ```
    *   Press `c` to toggle between the source code view and the callers/callees graph.
    *   Press `F1` at any time if you're unsure what the profile type means.

#### Recipe 2: Finding a Performance Regression (The Diff Workflow)
You suspect a recent commit made things slower. `pproftui`'s diffing is its superpower.

1.  **Get a profile from your `main` branch:**
    ```sh
    git checkout main
    go test -bench=. -cpuprofile=main.prof
    ```

2.  **Get a profile from your feature branch:**
    ```sh
    git checkout your-feature-branch
    go test -bench=. -cpuprofile=feature.prof
    ```

3.  **Diff them:**
    ```sh
    pproftui main.prof feature.prof
    ```
    `pproftui` will now show you the *delta*. Green (`+`) means more resources were used in `feature.prof`, red (`-`) means less. Use this to navigate the graph and find the exact function that introduced the new overhead.

#### Recipe 3: Profiling a Live Service
You want to see how your application behaves under load in a staging environment.

1.  **Ensure your app exposes pprof endpoints.** (e.g., by importing `net/http/pprof`)
2.  **Run `pproftui` in live mode:**
    ```sh
    # This will fetch a 5-second CPU profile every 10 seconds.
    pproftui -live="http://localhost:6060/debug/pprof/profile?seconds=5" -refresh=10s
    ```
    *   Press `space` to pause and resume the live updates.

#### Recipe 4: Cutting Through the Noise
Profiles are full of runtime and library code. Here's how to focus on what matters: **your code.**

```sh
# Provide your project's go module path
pproftui --module-path="github.com/your/project" cpu.prof
```
*   Your project's functions will now be marked with a `★`.
*   Press `p` to toggle "Project Only" mode, instantly hiding everything else.

---

## Keybindings

| Key         | Action                                                |
| :---------- | :---------------------------------------------------- |
| `↑`/`↓`     | Navigate the functions list                           |
| `t`         | Toggle profile type (`inuse_space`, `alloc_objects`)  |
| `c`         | Toggle between **c**ode and **c**all graph view       |
| `p`         | Toggle **p**roject-only code filter                   |
| `s`         | Cycle **s**ort order (`Self`, `Total`, `Name`)        |
| `f`         | Toggle **f**lame graph view                           |
| `Enter`     | *In flame graph:* Zoom in on selected function        |
| `Esc`       | *In flame graph:* Zoom out                            |
| `F1`        | Show detailed **help** and explanations               |
| `q` / `ctrl+c`| **Q**uit                                            |


---

## License

MIT