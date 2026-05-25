package timeout

import (
	"log"
	"sync"
	"time"

	"github.com/sudebaker/acb-go/internal/db"
)

// PendingTimeoutService periodically checks for tasks that have been in
// 'pending' status longer than the configured timeout and marks them as failed.
type PendingTimeoutService struct {
	repo          *db.TaskRepo
	timeoutMin    int
	checkInterval time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// NewPendingTimeoutService creates a new timeout service.
// timeoutMin: minutes a task can remain in 'pending' before being expired (0 = disabled).
// checkInterval: how often to check for expired tasks.
func NewPendingTimeoutService(repo *db.TaskRepo, timeoutMin int, checkInterval time.Duration) *PendingTimeoutService {
	return &PendingTimeoutService{
		repo:           repo,
		timeoutMin:     timeoutMin,
		checkInterval:  checkInterval,
		stopCh:         make(chan struct{}),
	}
}

// Start launches the background goroutine. Call Stop() to shut it down.
func (s *PendingTimeoutService) Start() {
	if s.timeoutMin <= 0 {
		log.Printf("[INFO] PendingTimeout: disabled (ACB_PENDING_TIMEOUT_MIN=0)")
		return
	}
	log.Printf("[INFO] PendingTimeout: started — timeout=%dm, checkInterval=%v", s.timeoutMin, s.checkInterval)
	s.wg.Add(1)
	go s.run()
}

// Stop signals the background goroutine to exit and waits for it to finish.
func (s *PendingTimeoutService) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *PendingTimeoutService) run() {
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
			log.Printf("[INFO] PendingTimeout: stopped")
			return
		}
	}
}

func (s *PendingTimeoutService) check() {
	ids, err := s.repo.ExpirePendingTasks(s.timeoutMin)
	if err != nil {
		log.Printf("[ERROR] PendingTimeout: %v", err)
		return
	}
	if len(ids) > 0 {
		log.Printf("[INFO] PendingTimeout: expired %d task(s): %v", len(ids), ids)
	}
}