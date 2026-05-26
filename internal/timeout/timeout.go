package timeout

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
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
		log.Info().Msg("timeout: disabled (all timeouts are 0)")
		return
	}
	log.Info().Int("pendingTimeoutMin", s.pendingTimeoutMin).Int("taskTimeoutMin", s.taskTimeoutMin).Int("agentStaleMin", s.agentStaleMin).Dur("checkInterval", s.checkInterval).Msg("timeout: started")
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
			log.Info().Msg("timeout: stopped")
			return
		}
	}
}

func (s *TimeoutService) check() {
	ctx := context.Background()
	if s.pendingTimeoutMin > 0 {
		ids, err := s.repo.ExpirePendingTasks(ctx, s.pendingTimeoutMin)
		if err != nil {
			log.Error().Err(err).Msg("timeout: pending check failed")
		} else if len(ids) > 0 {
			log.Info().Strs("taskIDs", ids).Int("count", len(ids)).Msg("timeout: expired pending tasks")
		}
	}

	if s.taskTimeoutMin > 0 {
		ids, err := s.repo.ExpireStaleInProgressTasks(ctx, s.taskTimeoutMin)
		if err != nil {
			log.Error().Err(err).Msg("timeout: task heartbeat check failed")
		} else if len(ids) > 0 {
			log.Info().Strs("taskIDs", ids).Int("count", len(ids)).Msg("timeout: expired stale in-progress tasks")
		}
	}

	if s.agentStaleMin > 0 && s.agentRepo != nil {
		staleDur := time.Duration(s.agentStaleMin) * time.Minute
		staleAgents, err := s.agentRepo.ListStale(ctx, staleDur)
		if err != nil {
			log.Error().Err(err).Msg("timeout: stale agents check failed")
			return
		}
		for _, agent := range staleAgents {
			released, err := s.repo.ReleaseAgentTasks(ctx, agent.Name)
			if err != nil {
				log.Error().Err(err).Str("agent", agent.Name).Msg("timeout: release tasks for stale agent failed")
				continue
			}
			if len(released) > 0 {
				log.Info().Int("count", len(released)).Str("agent", agent.Name).Msg("timeout: released tasks from stale agent")
			}
		}
	}
}