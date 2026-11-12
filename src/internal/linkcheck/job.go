package linkcheck

import (
	"context"
	"sync"
	"time"
)

// Job states.
const (
	Running   = "running"
	Paused    = "paused"
	Cancelled = "cancelled"
	Done      = "done"
)

// Job is one run over many URLs: checked a few at a time, interleaved by site, pausable and cancellable. Each result
// goes to the onResult callback as it arrives (the server stores it there), and stays readable from the job.
type Job struct {
	ID      string
	Total   int
	Started time.Time

	mu      sync.Mutex
	cond    *sync.Cond
	state   string
	checked int
	ended   time.Time
	results map[string]Result
	cancel  context.CancelFunc
	done    chan struct{}
}

// View is a job's progress, safe to hand out.
type View struct {
	ID      string    `json:"id"`
	State   string    `json:"state"`
	Total   int       `json:"total"`
	Checked int       `json:"checked"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitzero"`
	Done    bool      `json:"done"`
}

// Start checks urls (duplicates dropped) with `workers` checks at once overall; the limiter still keeps each site to
// its own budget.
func (c *Checker) Start(id string, urls []string, workers int, onResult func(Result)) *Job {
	seen := map[string]bool{}
	todo := []string{}
	for _, u := range urls {
		if !seen[u] {
			seen[u] = true
			todo = append(todo, u)
		}
	}
	todo = Interleave(todo)
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{ID: id, Total: len(todo), Started: c.Now().UTC(), state: Running, results: map[string]Result{}, cancel: cancel, done: make(chan struct{})}
	j.cond = sync.NewCond(&j.mu)
	queue := make(chan string)
	go func() {
		defer close(queue)
		for _, u := range todo {
			if !j.waitWhilePaused() {
				return
			}
			select {
			case queue <- u:
			case <-ctx.Done():
				return
			}
		}
	}()
	var wg sync.WaitGroup
	for range max(1, workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range queue {
				if !j.waitWhilePaused() { // checked here, not only when queued, so a pause holds every check not yet started
					return
				}
				r := c.Check(ctx, u)
				if ctx.Err() != nil {
					return // cancelled mid-check: an interrupted result says nothing
				}
				j.mu.Lock()
				j.results[u] = r
				j.checked++
				j.mu.Unlock()
				if onResult != nil {
					onResult(r)
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		j.mu.Lock()
		if j.state != Cancelled {
			j.state = Done
		}
		j.ended = c.Now().UTC()
		j.mu.Unlock()
		close(j.done)
	}()
	return j
}

// waitWhilePaused blocks while the job is paused; false once it is cancelled.
func (j *Job) waitWhilePaused() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	for j.state == Paused {
		j.cond.Wait()
	}
	return j.state == Running
}

// Pause stops new checks from starting; the ones under way finish.
func (j *Job) Pause() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state != Running {
		return false
	}
	j.state = Paused
	return true
}

func (j *Job) Resume() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state != Paused {
		return false
	}
	j.state = Running
	j.cond.Broadcast()
	return true
}

// Cancel stops the job; what was checked so far stays.
func (j *Job) Cancel() bool {
	j.mu.Lock()
	if j.state != Running && j.state != Paused {
		j.mu.Unlock()
		return false
	}
	j.state = Cancelled
	j.cond.Broadcast()
	j.mu.Unlock()
	j.cancel()
	return true
}

func (j *Job) View() View {
	j.mu.Lock()
	defer j.mu.Unlock()
	return View{ID: j.ID, State: j.state, Total: j.Total, Checked: j.checked, Started: j.Started, Ended: j.ended, Done: !j.ended.IsZero()}
}

// Results is a copy of everything checked so far, by URL.
func (j *Job) Results() map[string]Result {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make(map[string]Result, len(j.results))
	for k, v := range j.results {
		out[k] = v
	}
	return out
}

// Wait blocks until the job has stopped.
func (j *Job) Wait() { <-j.done }
