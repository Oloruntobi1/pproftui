# pproftui: Profiling Without Losing Your Mind

Learning about profiling, and specifically the golang pprof tool for the first time is always exciting. You think
finally i can be that person that can spot performance issues...i can be a 10x developer!

Then you use it...

The drop in excitement for me was quite **discouraging**, because here i am, someone, that wants to get more into performance so i can improve my app or code or just learning really, but somehow, with all the unclear terminologies like `cum`, `flat`, `samples`, and `percentages` besides numbers that all don’t immediately make sense — you spend more time googling these things than even profiling what you have.

Oh and the context switch between the web and back to your code!

Well, it's now in your terminal, with a very useful help button when you hit F1.

This is the most important feature for me. The rest you will discover.

---

## 🧾 License

MIT LICENSED.

---

## ⚡ Quick Usage

```sh
git https://clone github.com/Oloruntobi1/pproftui.git

cd pproftui

go build -o pproftui

./pproftui cpu.prof
```

or

```sh
./pproftui mem.prof
```

To compare/diff two different profiles:

```sh
./pproftui before_cpu.prof after_cpu.prof
```

For **live profiling**:

```sh
./pproftui http://localhost:6060/debug/pprof/profile?seconds=10
```

> (If your app is idle, simulate some work while this is running)

---

## 🧭 Hotkeys

* `t` — toggle view (e.g. between all heap stuff)
* `f` — flame graph (press `Enter` to drill down)
* `c` — see callers and callees
* `s` — sort

---

## More Screenshots?

Check the `screenshots` folder for screenshots.

---

More profiles will be supported sometime later. Cheers.
