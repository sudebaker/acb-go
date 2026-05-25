package timeout

import (
	"log"
	"sync"
	"time"

	"github.com/sudebaker/acb-go/internal/db"
)

// TimeoutService periodically checks for stalled tasks and marks them as failed.
// It handles two timeout scenarios:
//   - pending timeout: unclaimed tasks in 'pending' beyond the configured limit
//   - task heartbeat timeout: in-progress tasks with stale heartbeats
type TimeoutService struct {
	repo              *db.TaskRepo
	agentRepo         *db.AgentRepo
	pendingTimeoutMin int
	taskTimeoutMin    int
	agentStaleMin     int
	checkInterval     time.Duration
	stopCh            chan struct{}
	wg                sync.WaitGroup
}

// NewTimeoutService creates a new timeout service.
// pendingTimeoutMin: minutes a task can remain in 'pending' before being expired (0 = disabled).
// taskTimeoutMin: minutes a task can be in 'in_progress' without heartbeat (0 = disabled).
// agentStaleMin: minutes without agent heartbeat before releasing its tasks (0 = disabled).
// checkInterval: how often to check for expired tasks.
func NewTimeoutService(repo *db.TaskRepo, agentRepo *db.AgentRepo, pendingTimeoutMin, taskTimeoutMin, agentStaleMin int, checkInterval time.Duration) *TimeoutService {
	return &TimeoutService{
		repo:              repo,
		agentRepo:         agentRepo,
		pendingTimeoutMin: pendingTimeoutMin,
		taskTimeoutMin:    taskTimeoutMin,
		agentStaleMin:     agentStaleMin,
		checkInterval:     checkInterval,
		stopCh:            make(chan struct{}),
	}
}

// Start launches the background goroutine. Call Stop() to shut it down.
func (s *TimeoutService) Start() {
	if s.pendingTimeoutMin <= 0 && s.taskTimeoutMin <= 0 && s.agentStaleMin <= 0 {
		log.Printf("[INFO] TimeoutService: disabled (all timeouts are 0)")
		return
	}
	log.Printf("[INFO] TimeoutService: started — pendingTimeout=%dm, taskTimeout=%dm, agentStale=%dm, checkInterval=%v",
		s.pendingTimeoutMin, s.taskTimeoutMin, s.agentStaleMin, s.checkInterval)
	s.wg.Add(1)
	go s.run()
}

// Stop signals the background goroutine to exit and waits for it to finish.
func (s *TimeoutService) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *TimeoutService) run() {
	defer s.wg.Done()
	// Run once immediately on start
	s.check()

	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.check()
		case <-s.stopCh:
			log.Printf("[INFO] TimeoutService: stopped")
			return
		}
	}
}

func (s *TimeoutService) check() {
	if s.pendingTimeoutMin > 0 {
		ids, err := s.repo.ExpirePendingTasks(s.pendingTimeoutMin)
		if err != nil {
			log.Printf("[ERROR] TimeoutService (pending): %v", err)
		} else if len(ids) > 0 {
			log.Printf("[INFO] TimeoutService: expired %d pending task(s): %v", len(ids), ids)
		}
	}

	if s.taskTimeoutMin > 0 {
		ids, err := s.repo.ExpireStaleInProgressTasks(s.taskTimeoutMin)
		if err != nil {
			log.Printf("[ERROR] TimeoutService (task heartbeat): %v", err)
		} else if len(ids) > 0 {
			log.Printf("[INFO] TimeoutService: expired %d stale in-progress task(s): %v", len(ids), ids)
		}
	}

	if s.agentStaleMin > 0 && s.agentRepo != nil {
		staleDur := time.Duration(s.agentStaleMin) * time.Minute
		staleAgents, err := s.agentRepo.ListStale(staleDur)
		if err != nil {
			log.Printf("[ERROR] TimeoutService (stale agents): %v", err)
			return
		}
		for _, agent := range staleAgents {
			released, err := s.repo.ReleaseAgentTasks(agent.Name)
			if err != nil {
				log.Printf("[ERROR] TimeoutService: release tasks for stale agent %s: %v", agent.Name, err)
				continue
			}
			if len(released) > 0 {
				log.Printf("[INFO] TimeoutService: released %d task(s) from stale agent %s", len(released), agent.Name)
			}
		}
	}
}