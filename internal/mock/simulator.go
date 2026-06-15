package mock

import (
	"fmt"
	"gps/internal/model"
	"gps/internal/sse"
	"gps/internal/store"
	"math/rand"
	"sync"
	"time"
)

// Simulator drives a release plan through phases with realistic timing
type Simulator struct {
	store   store.Store
	broker  *sse.Broker
	mu      sync.Mutex
	running map[string]chan struct{} // planID -> cancel channel
}

func NewSimulator(store store.Store, broker *sse.Broker) *Simulator {
	return &Simulator{
		store:   store,
		broker:  broker,
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
func (sim *Simulator) RetryModule(planID, moduleID string) error {
	plan := sim.store.GetPlan(planID)
	if plan == nil {
		return fmt.Errorf("plan not found")
	}

	found := false
	for _, m := range plan.Modules {
		if m.ModuleID == moduleID {
			if m.Status != model.StatusFailed {
				return fmt.Errorf("module %s is not in FAILED status (current: %s)", moduleID, m.Status)
			}
			if m.Kind != model.KindInternal {
				return fmt.Errorf("module %s is not an internal module", moduleID)
			}
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("module %s not found in plan", moduleID)
	}

	sim.store.SetPlanRunning(planID)
	sim.store.SetPlanPhase(planID, model.PhaseReleasing)
	sim.broker.Broadcast(planID, model.SSEEvent{
		Type: "phase_change", Data: model.PhaseChangeEvent{Phase: model.PhaseReleasing},
	})

	go sim.retryModuleAsync(planID, moduleID)
	return nil
}

func (sim *Simulator) retryModuleAsync(planID, moduleID string) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	cancel := make(chan struct{})

	sim.broker.Broadcast(planID, model.SSEEvent{
		Type: "module_log", Data: model.ModuleLogEvent{
			ModuleID: moduleID, Line: "=== Manual retry triggered ===", Timestamp: time.Now().UnixMilli(),
		},
	})

	success := sim.releaseModule(planID, moduleID, rng, cancel, 0)

	if success {
		progress := sim.store.GetProgress(planID)
		if progress != nil && progress.Releasing == 0 && progress.Pending == 0 && progress.Tagged == 0 {
			finalStatus := model.PlanCompleted
			if progress.Failed > 0 {
				finalStatus = model.PlanAborted
			}
			sim.store.CompletePlan(planID, finalStatus)
			sim.broker.Broadcast(planID, model.SSEEvent{
				Type: "plan_complete", Data: model.PlanCompleteEvent{
					Status: finalStatus, Succeeded: progress.Succeeded, Failed: progress.Failed, Skipped: progress.Skipped,
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

	// Phase 2.5: Pending-External Confirmation Gate
	if !sim.phaseGatePendingExternal(planID, cancel) {
		return
	}

	// Phase 3: Concurrent Pool Release
	if !sim.phaseReleasing(planID, cancel, rng) {
		return
	}

	// Phase 4: Complete
	sim.store.SetPlanPhase(planID, model.PhaseCompleted)
	sim.broker.Broadcast(planID, model.SSEEvent{
		Type: "phase_change", Data: model.PhaseChangeEvent{Phase: model.PhaseCompleted},
	})

	progress := sim.store.GetProgress(planID)
	finalStatus := model.PlanCompleted
	if progress.Failed > 0 {
		finalStatus = model.PlanAborted
	}

	sim.store.CompletePlan(planID, finalStatus)
	sim.broker.Broadcast(planID, model.SSEEvent{
		Type: "plan_complete", Data: model.PlanCompleteEvent{
			Status: finalStatus, Succeeded: progress.Succeeded, Failed: progress.Failed, Skipped: progress.Skipped,
		},
	})
}

// phaseGatePendingExternal waits for all pending-external modules to be confirmed.
// In the mock, pending-external modules are pre-set to SUCCESS at ConfirmPlan time,
// so this phase completes immediately. In a real system, this would poll/wait for
// human confirmation via the confirm-external API.
func (sim *Simulator) phaseGatePendingExternal(planID string, cancel chan struct{}) bool {
	sim.store.SetPlanPhase(planID, model.PhaseGatePendingExternal)
	sim.broker.Broadcast(planID, model.SSEEvent{
		Type: "phase_change", Data: model.PhaseChangeEvent{Phase: model.PhaseGatePendingExternal},
	})

	plan := sim.store.GetPlan(planID)
	if plan == nil {
		return false
	}

	// Check if all pending-external modules are confirmed (SUCCESS).
	// In mock, they are pre-set to SUCCESS. Simulate a brief delay.
	pendingCount := 0
	for _, m := range plan.Modules {
		if m.Kind == model.KindPendingExternal && m.Status != model.StatusSuccess {
			pendingCount++
		}
	}

	if pendingCount > 0 {
		sim.broker.Broadcast(planID, model.SSEEvent{
			Type: "module_log", Data: model.ModuleLogEvent{
				ModuleID: "system", Line: fmt.Sprintf("Waiting for %d pending-external module(s) to be confirmed...", pendingCount),
				Timestamp: time.Now().UnixMilli(),
			},
		})
		// Poll until confirmed or cancelled.
		for {
			select {
			case <-cancel:
				sim.store.CompletePlan(planID, model.PlanAborted)
				return false
			case <-time.After(500 * time.Millisecond):
				plan = sim.store.GetPlan(planID)
				if plan == nil {
					return false
				}
				allConfirmed := true
				for _, m := range plan.Modules {
					if m.Kind == model.KindPendingExternal && m.Status != model.StatusSuccess {
						allConfirmed = false
						break
					}
				}
				if allConfirmed {
					sim.broker.Broadcast(planID, model.SSEEvent{
						Type: "module_log", Data: model.ModuleLogEvent{
							ModuleID: "system", Line: "All pending-external modules confirmed.",
							Timestamp: time.Now().UnixMilli(),
						},
					})
					return true
				}
			}
		}
	}

	sim.broker.Broadcast(planID, model.SSEEvent{
		Type: "module_log", Data: model.ModuleLogEvent{
			ModuleID: "system", Line: "No pending-external modules to confirm, proceeding.",
			Timestamp: time.Now().UnixMilli(),
		},
	})
	return true
}

func (sim *Simulator) phaseTagging(planID string, cancel chan struct{}, rng *rand.Rand) bool {
	sim.store.SetPlanPhase(planID, model.PhaseTagging)
	sim.broker.Broadcast(planID, model.SSEEvent{
		Type: "phase_change", Data: model.PhaseChangeEvent{Phase: model.PhaseTagging},
	})

	plan := sim.store.GetPlan(planID)
	if plan == nil {
		return false
	}

	// Group internal modules by repo for batch tagging.
	repoModules := make(map[string][]string)
	for _, m := range plan.Modules {
		if m.Kind == model.KindInternal {
			repoModules[m.RepoID] = append(repoModules[m.RepoID], m.ModuleID)
		}
	}

	for _, moduleIDs := range repoModules {
		select {
		case <-cancel:
			sim.store.CompletePlan(planID, model.PlanAborted)
			return false
		default:
		}

		delay := time.Duration(300+rng.Intn(500)) * time.Millisecond
		time.Sleep(delay)

		for _, mid := range moduleIDs {
			sim.store.SetModuleStatus(planID, mid, model.StatusTagged, "")
			sim.broker.Broadcast(planID, model.SSEEvent{
				Type: "module_status", Data: model.ModuleStatusEvent{ModuleID: mid, Status: model.StatusTagged},
			})
		}
	}

	return true
}

func (sim *Simulator) phaseAnalyzing(planID string, cancel chan struct{}) bool {
	sim.store.SetPlanPhase(planID, model.PhaseAnalyzing)
	sim.broker.Broadcast(planID, model.SSEEvent{
		Type: "phase_change", Data: model.PhaseChangeEvent{Phase: model.PhaseAnalyzing},
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
	sim.broker.Broadcast(planID, model.SSEEvent{
		Type: "phase_change", Data: model.PhaseChangeEvent{Phase: model.PhaseReleasing},
	})

	plan := sim.store.GetPlan(planID)
	if plan == nil || plan.DepGraph == nil {
		return false
	}

	concurrency := plan.Concurrency
	sortedOrder := plan.DepGraph.SortedOrder
	strategy := plan.FailureStrategy
	maxRetries := plan.MaxRetries

	// Build dependency lookup.
	depsOf := make(map[string][]string)
	for _, e := range plan.DepGraph.Edges {
		depsOf[e.To] = append(depsOf[e.To], e.From)
	}

	// Track statuses.
	statusMap := make(map[string]model.ModuleStatus)
	var statusMu sync.Mutex
	for _, mid := range sortedOrder {
		statusMap[mid] = model.StatusTagged
	}

	// Mark pending-external as already SUCCESS in the status map.
	for _, m := range plan.Modules {
		if m.Kind == model.KindPendingExternal {
			statusMap[m.ModuleID] = model.StatusSuccess
		}
	}

	aborted := false
	var abortMu sync.Mutex

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, moduleID := range sortedOrder {
		mid := moduleID

		// Skip pending-external modules (already confirmed).
		isPendingExternal := false
		for _, m := range plan.Modules {
			if m.ModuleID == mid && m.Kind == model.KindPendingExternal {
				isPendingExternal = true
				break
			}
		}
		if isPendingExternal {
			continue
		}

		// Check abort.
		abortMu.Lock()
		if aborted {
			abortMu.Unlock()
			sim.store.SetModuleStatus(planID, mid, model.StatusSkipped, "release aborted")
			sim.broker.Broadcast(planID, model.SSEEvent{
				Type: "module_status", Data: model.ModuleStatusEvent{ModuleID: mid, Status: model.StatusSkipped},
			})
			statusMu.Lock()
			statusMap[mid] = model.StatusSkipped
			statusMu.Unlock()
			continue
		}
		abortMu.Unlock()

		// Wait for upstream dependencies.
		if !sim.waitForUpstream(planID, mid, depsOf, statusMap, &statusMu, cancel) {
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
				sim.broker.Broadcast(planID, model.SSEEvent{
					Type: "module_status", Data: model.ModuleStatusEvent{ModuleID: mid, Status: model.StatusSkipped},
				})
				statusMu.Lock()
				statusMap[mid] = model.StatusSkipped
				statusMu.Unlock()
				continue
			}
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
				// Simulate akasha write-back for cross-repo published modules.
				sim.simulateAkashaWriteBack(planID, mid, plan)
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

// simulateAkashaWriteBack simulates writing a module's new version to akasha
// after successful release. Only for internal modules that are consumed cross-repo.
func (sim *Simulator) simulateAkashaWriteBack(planID, moduleID string, plan *model.ReleasePlan) {
	if plan.DepGraph == nil {
		return
	}

	// Check if this module has any outgoing cross-repo edges (it is "published").
	isPublished := false
	for _, e := range plan.DepGraph.Edges {
		if e.From == moduleID && e.CrossRepo {
			isPublished = true
			break
		}
	}
	if !isPublished {
		return
	}

	// Find the module's target version.
	var targetVersion string
	for _, m := range plan.Modules {
		if m.ModuleID == moduleID {
			targetVersion = m.TargetVersion
			break
		}
	}

	sim.broker.Broadcast(planID, model.SSEEvent{
		Type: "module_log", Data: model.ModuleLogEvent{
			ModuleID: moduleID,
			Line:     fmt.Sprintf("[akasha] Writing %s:%s to branch %s", moduleID, targetVersion, plan.DmsBranch),
			Timestamp: time.Now().UnixMilli(),
		},
	})
	time.Sleep(100 * time.Millisecond) // simulate network latency
	sim.broker.Broadcast(planID, model.SSEEvent{
		Type: "module_log", Data: model.ModuleLogEvent{
			ModuleID: moduleID,
			Line:     fmt.Sprintf("[akasha] Version registered successfully."),
			Timestamp: time.Now().UnixMilli(),
		},
	})
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
			sim.broker.Broadcast(planID, model.SSEEvent{
				Type: "module_status", Data: model.ModuleStatusEvent{ModuleID: moduleID, Status: model.StatusRetrying},
			})
			sim.broker.Broadcast(planID, model.SSEEvent{
				Type: "module_log", Data: model.ModuleLogEvent{
					ModuleID: moduleID, Line: fmt.Sprintf("=== Retry attempt %d/%d ===", attempt, maxRetries),
					Timestamp: time.Now().UnixMilli(),
				},
			})
			time.Sleep(500 * time.Millisecond)
		}

		sim.store.SetModuleStatus(planID, moduleID, model.StatusReleasing, "")
		sim.broker.Broadcast(planID, model.SSEEvent{
			Type: "module_status", Data: model.ModuleStatusEvent{ModuleID: moduleID, Status: model.StatusReleasing},
		})

		_ = pipelineID

		failAt := -1
		failChance := 10
		if attempt > 0 {
			failChance = 5
		}
		if rng.Intn(100) < failChance {
			failAt = 3 + rng.Intn(len(logLines)-3)
		}

		for i, line := range logLines {
			select {
			case <-cancel:
				return false
			default:
			}

			if i == failAt {
				errMsg := fmt.Sprintf("FAILED at step: %s - Build error: exit code 1", line)
				sim.broker.Broadcast(planID, model.SSEEvent{
					Type: "module_log", Data: model.ModuleLogEvent{
						ModuleID: moduleID, Line: "ERROR: " + errMsg, Timestamp: time.Now().UnixMilli(),
					},
				})

				if attempt == maxRetries {
					sim.store.SetModuleStatus(planID, moduleID, model.StatusFailed, errMsg)
					sim.broker.Broadcast(planID, model.SSEEvent{
						Type: "module_status", Data: model.ModuleStatusEvent{ModuleID: moduleID, Status: model.StatusFailed, ErrorMsg: errMsg},
					})
					return false
				}
				break
			}

			delay := time.Duration(400+rng.Intn(800)) * time.Millisecond
			time.Sleep(delay)

			sim.broker.Broadcast(planID, model.SSEEvent{
				Type: "module_log", Data: model.ModuleLogEvent{
					ModuleID: moduleID, Line: fmt.Sprintf("[%s] %s OK", moduleID, line),
					Timestamp: time.Now().UnixMilli(),
				},
			})

			if i == failAt {
				break
			}
		}

		if failAt == -1 {
			sim.store.SetModuleStatus(planID, moduleID, model.StatusSuccess, "")
			sim.broker.Broadcast(planID, model.SSEEvent{
				Type: "module_status", Data: model.ModuleStatusEvent{ModuleID: moduleID, Status: model.StatusSuccess},
			})
			return true
		}
	}

	return false
}
