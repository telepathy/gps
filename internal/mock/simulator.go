package mock

import (
	"fmt"
	"gps/internal/model"
	"math/rand"
	"sync"
	"time"
)

// Simulator drives a release plan through phases with realistic timing
type Simulator struct {
	store   *Store
	mu      sync.Mutex
	running map[string]chan struct{} // planID -> cancel channel
}

func NewSimulator(store *Store) *Simulator {
	return &Simulator{
		store:   store,
		running: make(map[string]chan struct{}),
	}
}

func (sim *Simulator) Start(planID string) error {
	sim.mu.Lock()
	defer sim.mu.Unlock()

	plan := sim.store.GetPlan(planID)
	if plan == nil {
		return fmt.Errorf("plan not found")
	}
	if plan.Status != model.PlanConfirmed {
		return fmt.Errorf("plan must be CONFIRMED to execute")
	}

	cancel := make(chan struct{})
	sim.running[planID] = cancel

	sim.store.SetPlanRunning(planID)

	go sim.run(planID, cancel)
	return nil
}

func (sim *Simulator) Abort(planID string) {
	sim.mu.Lock()
	defer sim.mu.Unlock()
	if ch, ok := sim.running[planID]; ok {
		close(ch)
		delete(sim.running, planID)
	}
}

// RetryModule re-runs a single failed module asynchronously.
// It resets the module to RELEASING and runs the release pipeline in a goroutine.
func (sim *Simulator) RetryModule(planID, moduleID string) error {
	plan := sim.store.GetPlan(planID)
	if plan == nil {
		return fmt.Errorf("plan not found")
	}

	// Verify module exists and is in FAILED state
	found := false
	for _, m := range plan.Modules {
		if m.ModuleID == moduleID {
			if m.Status != model.StatusFailed {
				return fmt.Errorf("module %s is not in FAILED status (current: %s)", moduleID, m.Status)
			}
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("module %s not found in plan", moduleID)
	}

	// Ensure the plan stays in RUNNING state if it was completed/aborted
	sim.store.SetPlanRunning(planID)
	sim.store.SetPlanPhase(planID, model.PhaseReleasing)
	sim.store.Broadcast(planID, model.SSEEvent{
		Type: "phase_change",
		Data: model.PhaseChangeEvent{Phase: model.PhaseReleasing},
	})

	go sim.retryModuleAsync(planID, moduleID)
	return nil
}

func (sim *Simulator) retryModuleAsync(planID, moduleID string) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	cancel := make(chan struct{}) // non-cancellable for manual retry

	sim.store.Broadcast(planID, model.SSEEvent{
		Type: "module_log",
		Data: model.ModuleLogEvent{
			ModuleID:  moduleID,
			Line:      "=== Manual retry triggered ===",
			Timestamp: time.Now().UnixMilli(),
		},
	})

	success := sim.releaseModule(planID, moduleID, rng, cancel, 0)

	if success {
		// Check if all modules are now terminal (SUCCESS/FAILED/SKIPPED)
		progress := sim.store.GetProgress(planID)
		if progress != nil && progress.Releasing == 0 && progress.Pending == 0 && progress.Tagged == 0 {
			finalStatus := model.PlanCompleted
			if progress.Failed > 0 {
				finalStatus = model.PlanAborted
			}
			sim.store.CompletePlan(planID, finalStatus)
			sim.store.Broadcast(planID, model.SSEEvent{
				Type: "plan_complete",
				Data: model.PlanCompleteEvent{
					Status:    finalStatus,
					Succeeded: progress.Succeeded,
					Failed:    progress.Failed,
					Skipped:   progress.Skipped,
				},
			})
		}
	}
}

func (sim *Simulator) run(planID string, cancel chan struct{}) {
	defer func() {
		sim.mu.Lock()
		delete(sim.running, planID)
		sim.mu.Unlock()
	}()

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Phase 1: Tagging
	if !sim.phaseTagging(planID, cancel, rng) {
		return
	}

	// Phase 2: Dependency Analysis
	if !sim.phaseAnalyzing(planID, cancel) {
		return
	}

	// Phase 3: Concurrent Pool Release
	if !sim.phaseReleasing(planID, cancel, rng) {
		return
	}

	// Phase 4: Complete
	sim.store.SetPlanPhase(planID, model.PhaseCompleted)
	sim.store.Broadcast(planID, model.SSEEvent{
		Type: "phase_change",
		Data: model.PhaseChangeEvent{Phase: model.PhaseCompleted},
	})

	// Determine final status
	progress := sim.store.GetProgress(planID)
	finalStatus := model.PlanCompleted
	if progress.Failed > 0 {
		finalStatus = model.PlanAborted
	}

	sim.store.CompletePlan(planID, finalStatus)
	sim.store.Broadcast(planID, model.SSEEvent{
		Type: "plan_complete",
		Data: model.PlanCompleteEvent{
			Status:    finalStatus,
			Succeeded: progress.Succeeded,
			Failed:    progress.Failed,
			Skipped:   progress.Skipped,
		},
	})
}

func (sim *Simulator) phaseTagging(planID string, cancel chan struct{}, rng *rand.Rand) bool {
	sim.store.SetPlanPhase(planID, model.PhaseTagging)
	sim.store.Broadcast(planID, model.SSEEvent{
		Type: "phase_change",
		Data: model.PhaseChangeEvent{Phase: model.PhaseTagging},
	})

	plan := sim.store.GetPlan(planID)
	if plan == nil {
		return false
	}

	// Group modules by repo for batch tagging
	repoModules := make(map[string][]string)
	for _, m := range plan.Modules {
		repoModules[m.RepoID] = append(repoModules[m.RepoID], m.ModuleID)
	}

	for _, moduleIDs := range repoModules {
		select {
		case <-cancel:
			sim.store.CompletePlan(planID, model.PlanAborted)
			return false
		default:
		}

		// Tag all modules in this repo
		delay := time.Duration(300+rng.Intn(500)) * time.Millisecond
		time.Sleep(delay)

		for _, mid := range moduleIDs {
			sim.store.SetModuleStatus(planID, mid, model.StatusTagged, "")
			sim.store.Broadcast(planID, model.SSEEvent{
				Type: "module_status",
				Data: model.ModuleStatusEvent{ModuleID: mid, Status: model.StatusTagged},
			})
		}
	}

	return true
}

func (sim *Simulator) phaseAnalyzing(planID string, cancel chan struct{}) bool {
	sim.store.SetPlanPhase(planID, model.PhaseAnalyzing)
	sim.store.Broadcast(planID, model.SSEEvent{
		Type: "phase_change",
		Data: model.PhaseChangeEvent{Phase: model.PhaseAnalyzing},
	})

	select {
	case <-cancel:
		sim.store.CompletePlan(planID, model.PlanAborted)
		return false
	case <-time.After(2 * time.Second):
	}

	return true
}

func (sim *Simulator) phaseReleasing(planID string, cancel chan struct{}, rng *rand.Rand) bool {
	sim.store.SetPlanPhase(planID, model.PhaseReleasing)
	sim.store.Broadcast(planID, model.SSEEvent{
		Type: "phase_change",
		Data: model.PhaseChangeEvent{Phase: model.PhaseReleasing},
	})

	plan := sim.store.GetPlan(planID)
	if plan == nil || plan.DepGraph == nil {
		return false
	}

	concurrency := plan.Concurrency
	sortedOrder := plan.DepGraph.SortedOrder
	strategy := plan.FailureStrategy
	maxRetries := plan.MaxRetries

	// Build dependency lookup
	depsOf := make(map[string][]string) // module -> list of dependencies
	for _, e := range plan.DepGraph.Edges {
		depsOf[e.To] = append(depsOf[e.To], e.From)
	}

	// Track statuses
	statusMap := make(map[string]model.ModuleStatus)
	var statusMu sync.Mutex
	for _, mid := range sortedOrder {
		statusMap[mid] = model.StatusTagged
	}

	aborted := false
	var abortMu sync.Mutex

	// Semaphore for concurrency pool
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, moduleID := range sortedOrder {
		mid := moduleID

		// Check abort
		abortMu.Lock()
		if aborted {
			abortMu.Unlock()
			sim.store.SetModuleStatus(planID, mid, model.StatusSkipped, "release aborted")
			sim.store.Broadcast(planID, model.SSEEvent{
				Type: "module_status",
				Data: model.ModuleStatusEvent{ModuleID: mid, Status: model.StatusSkipped},
			})
			statusMu.Lock()
			statusMap[mid] = model.StatusSkipped
			statusMu.Unlock()
			continue
		}
		abortMu.Unlock()

		// Wait for upstream dependencies
		if !sim.waitForUpstream(planID, mid, depsOf, statusMap, &statusMu, cancel) {
			// Check if upstream failed
			statusMu.Lock()
			upstreamFailed := false
			for _, dep := range depsOf[mid] {
				if statusMap[dep] == model.StatusFailed || statusMap[dep] == model.StatusSkipped {
					upstreamFailed = true
					break
				}
			}
			statusMu.Unlock()

			if upstreamFailed {
				sim.store.SetModuleStatus(planID, mid, model.StatusSkipped, "upstream dependency failed")
				sim.store.Broadcast(planID, model.SSEEvent{
					Type: "module_status",
					Data: model.ModuleStatusEvent{ModuleID: mid, Status: model.StatusSkipped},
				})
				statusMu.Lock()
				statusMap[mid] = model.StatusSkipped
				statusMu.Unlock()
				continue
			}

			// Cancelled
			return false
		}

		select {
		case <-cancel:
			sim.store.CompletePlan(planID, model.PlanAborted)
			return false
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			success := sim.releaseModule(planID, mid, rng, cancel, maxRetries)

			statusMu.Lock()
			if success {
				statusMap[mid] = model.StatusSuccess
			} else {
				statusMap[mid] = model.StatusFailed
				if strategy == model.StrategyAbort {
					abortMu.Lock()
					aborted = true
					abortMu.Unlock()
				}
			}
			statusMu.Unlock()
		}()
	}

	wg.Wait()
	return true
}

func (sim *Simulator) waitForUpstream(planID, moduleID string, depsOf map[string][]string, statusMap map[string]model.ModuleStatus, statusMu *sync.Mutex, cancel chan struct{}) bool {
	deps := depsOf[moduleID]
	if len(deps) == 0 {
		return true
	}

	for {
		select {
		case <-cancel:
			return false
		default:
		}

		statusMu.Lock()
		allDone := true
		anyFailed := false
		for _, dep := range deps {
			s := statusMap[dep]
			if s == model.StatusFailed || s == model.StatusSkipped {
				anyFailed = true
				break
			}
			if s != model.StatusSuccess {
				allDone = false
			}
		}
		statusMu.Unlock()

		if anyFailed {
			return false
		}
		if allDone {
			return true
		}

		time.Sleep(200 * time.Millisecond)
	}
}

func (sim *Simulator) releaseModule(planID, moduleID string, rng *rand.Rand, cancel chan struct{}, maxRetries int) bool {
	pipelineID := fmt.Sprintf("pipe-%s-%d", moduleID, time.Now().UnixMilli())

	logLines := []string{
		"Cloning repository...",
		"Resolving dependencies...",
		"Compiling sources...",
		"Running unit tests...",
		"Packaging artifacts...",
		"Publishing to repository...",
		"Verifying deployment...",
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			sim.store.SetModuleStatus(planID, moduleID, model.StatusRetrying, "")
			sim.store.Broadcast(planID, model.SSEEvent{
				Type: "module_status",
				Data: model.ModuleStatusEvent{ModuleID: moduleID, Status: model.StatusRetrying},
			})
			sim.store.Broadcast(planID, model.SSEEvent{
				Type: "module_log",
				Data: model.ModuleLogEvent{
					ModuleID:  moduleID,
					Line:      fmt.Sprintf("=== Retry attempt %d/%d ===", attempt, maxRetries),
					Timestamp: time.Now().UnixMilli(),
				},
			})
			time.Sleep(500 * time.Millisecond)
		}

		sim.store.SetModuleStatus(planID, moduleID, model.StatusReleasing, "")
		sim.store.Broadcast(planID, model.SSEEvent{
			Type: "module_status",
			Data: model.ModuleStatusEvent{ModuleID: moduleID, Status: model.StatusReleasing},
		})

		_ = pipelineID

		// Simulate log output
		failAt := -1
		// 10% chance of failure on first attempt, 5% on retries
		failChance := 10
		if attempt > 0 {
			failChance = 5
		}
		if rng.Intn(100) < failChance {
			failAt = 3 + rng.Intn(len(logLines)-3) // fail during test/package/publish
		}

		for i, line := range logLines {
			select {
			case <-cancel:
				return false
			default:
			}

			if i == failAt {
				errMsg := fmt.Sprintf("FAILED at step: %s - Build error: exit code 1", line)
				sim.store.Broadcast(planID, model.SSEEvent{
					Type: "module_log",
					Data: model.ModuleLogEvent{
						ModuleID:  moduleID,
						Line:      "ERROR: " + errMsg,
						Timestamp: time.Now().UnixMilli(),
					},
				})

				if attempt == maxRetries {
					sim.store.SetModuleStatus(planID, moduleID, model.StatusFailed, errMsg)
					sim.store.Broadcast(planID, model.SSEEvent{
						Type: "module_status",
						Data: model.ModuleStatusEvent{ModuleID: moduleID, Status: model.StatusFailed, ErrorMsg: errMsg},
					})
					return false
				}
				break
			}

			delay := time.Duration(400+rng.Intn(800)) * time.Millisecond
			time.Sleep(delay)

			sim.store.Broadcast(planID, model.SSEEvent{
				Type: "module_log",
				Data: model.ModuleLogEvent{
					ModuleID:  moduleID,
					Line:      fmt.Sprintf("[%s] %s OK", moduleID, line),
					Timestamp: time.Now().UnixMilli(),
				},
			})

			if i == failAt {
				break // already handled
			}
		}

		if failAt == -1 {
			// Success
			sim.store.SetModuleStatus(planID, moduleID, model.StatusSuccess, "")
			sim.store.Broadcast(planID, model.SSEEvent{
				Type: "module_status",
				Data: model.ModuleStatusEvent{ModuleID: moduleID, Status: model.StatusSuccess},
			})
			return true
		}
	}

	return false
}
