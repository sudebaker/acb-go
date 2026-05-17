package dispatcher

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/models"
)

const (
	retryQueuePrefix = "acb:retry:"
	maxRetries       = 5
	httpTimeout      = 15 * time.Second // total timeout
	httpConnectTimeout = 5 * time.Second // TCP connect alone
)

// retryEntry stores a pending retry in Redis.
type retryEntry struct {
	TaskID    string `json:"task_id"`
	AgentName string `json:"agent_name"`
	Attempt   int    `json:"attempt"`
}

// WebhookPayload is the JSON body sent to agent webhook URLs.
type WebhookPayload struct {
	Action    string      `json:"action"`
	Task      models.Task `json:"task"`
	Timestamp string      `json:"timestamp"`
	// Optional: previous state info for status change notifications
	OldStatus string      `json:"old_status,omitempty"`
	NewStatus string      `json:"new_status,omitempty"`
}

// HTTPDoer abstracts http.Client.Do for testability.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Dispatcher pushes tasks to agent webhooks and retries on failure.
type Dispatcher struct {
	semaphore chan struct{}
	agentRepo  *db.AgentRepo
	taskRepo    *db.TaskRepo
	rdb         *goredis.Client
	httpClient  HTTPDoer
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// NewDispatcher creates a Dispatcher. If rdb is nil, retry queue is disabled.
// The HTTP client is configured with SSRF protections (disabled redirects, timeouts).
func NewDispatcher(agentRepo *db.AgentRepo, taskRepo *db.TaskRepo, rdb *goredis.Client) *Dispatcher {
	return &Dispatcher{
		agentRepo:  agentRepo,
		taskRepo:   taskRepo,
		rdb:        rdb,
		httpClient: NewSafeHTTPClient(),
		semaphore:  make(chan struct{}, 10),
	}
}

// Start begins the background retry goroutine.
func (d *Dispatcher) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	d.wg.Add(1)
	go d.processRetryQueue(ctx)
	log.Println("[dispatcher] started, processing retry queue")
}

// Stop shuts down the retry goroutine.
func (d *Dispatcher) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.wg.Wait()
	log.Println("[dispatcher] stopped")
}

// DispatchNewTask is called when a new task is created.
// It finds matching agents and POSTs the task to their webhook URLs.
func (d *Dispatcher) DispatchNewTask(task *models.Task) {
	agents, err := d.agentRepo.FindMatchingAgents(task.RequiredSkills)
	if err != nil {
		log.Printf("[dispatcher] error finding matching agents: %v", err)
		return
	}

	for i := range agents {
		agent := agents[i]
		if agent.WebhookURL == "" {
			continue
		}
		go d.sendWebhookWithSemaphore(agent, task, 0)
	}
}

// NotifyStatusChange sends a webhook to the assigned agent when task status changes.
// Actions: "task_claimed", "task_started", "task_blocked", "task_completed", "task_failed"
func (d *Dispatcher) NotifyStatusChange(task *models.Task, oldStatus, newStatus, action string) {
	if task.Assignee == "" {
		return // No agent to notify
	}

	agent, err := d.agentRepo.GetByName(task.Assignee)
	if err != nil {
		log.Printf("[dispatcher] error getting agent %s: %v", task.Assignee, err)
		return
	}
	if agent == nil || agent.WebhookURL == "" {
		return // Agent not found or no webhook configured
	}

	go d.sendStatusWebhook(*agent, task, oldStatus, newStatus, action)
}

// sendStatusWebhook sends a status change notification to a single agent.
func (d *Dispatcher) sendStatusWebhook(agent models.Agent, task *models.Task, oldStatus, newStatus, action string) {
	d.semaphore <- struct{}{}
	defer func() {
		<-d.semaphore
	}()

	// Validate webhook URL before sending
	if err := ValidateWebhookURL(agent.WebhookURL); err != nil {
		log.Printf("[dispatcher] invalid webhook URL for agent %s: %v", agent.Name, err)
		return
	}

	payload := WebhookPayload{
		Action:    action,
		Task:      *task,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		OldStatus: oldStatus,
		NewStatus: newStatus,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[dispatcher] marshal payload for agent %s: %v", agent.Name, err)
		return
	}

	// Create request with timestamp header
	req, err := http.NewRequest("POST", agent.WebhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[dispatcher] create request for agent %s: %v", agent.Name, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Timestamp", payload.Timestamp)

	// Sign with HMAC-SHA256 of the body
	if agent.WebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(agent.WebhookSecret))
		mac.Write(body)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Webhook-Signature", sig)
	}

	// Set timeout for this request
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		log.Printf("[dispatcher] status webhook to agent %s failed: %v", agent.Name, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("[dispatcher] status webhook to agent %s returned status %d", agent.Name, resp.StatusCode)
		return
	}
	log.Printf("[dispatcher] status webhook to agent %s succeeded (action=%s, status=%s)", agent.Name, action, newStatus)
}

// sendWebhook POSTs the task to an agent's webhook URL.
// On failure, it enqueues a retry via Redis.
// Validates webhook URL for SSRF, adds timestamp to HMAC.
func (d *Dispatcher) sendWebhookWithSemaphore(agent models.Agent, task *models.Task, attempt int) {
	d.semaphore <- struct{}{}
	defer func() {
		<-d.semaphore
	}()
	d.sendWebhook(agent, task, attempt)
}

func (d *Dispatcher) sendWebhook(agent models.Agent, task *models.Task, attempt int) {
	d.wg.Add(1)
	defer d.wg.Done()
	// Validate webhook URL before sending
	if err := ValidateWebhookURL(agent.WebhookURL); err != nil {
		log.Printf("[dispatcher] invalid webhook URL for agent %s: %v", agent.Name, err)
		return
	}

	payload := WebhookPayload{
		Action:    "new_task",
		Task:       *task,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[dispatcher] marshal payload for agent %s: %v", agent.Name, err)
		return
	}

	// Create request with timestamp header
	req, err := http.NewRequest("POST", agent.WebhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[dispatcher] create request for agent %s: %v", agent.Name, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Timestamp", payload.Timestamp)

	// Sign with HMAC-SHA256 of the body (matches Hermes webhook validation)
	if agent.WebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(agent.WebhookSecret))
		mac.Write(body)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Webhook-Signature", sig)
	}

	// Set timeout for this request (5s connect + 10s body)
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		log.Printf("[dispatcher] webhook to agent %s failed (attempt %d): %v", agent.Name, attempt+1, err)
		d.enqueueRetry(agent.Name, task.ID, attempt)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("[dispatcher] webhook to agent %s returned status %d (attempt %d)", agent.Name, resp.StatusCode, attempt+1)
		d.enqueueRetry(agent.Name, task.ID, attempt)
		return
	}
	log.Printf("[dispatcher] webhook to agent %s succeeded (status %d)", agent.Name, resp.StatusCode)
}

// enqueueRetry pushes a failed task dispatch to Redis for later retry.
func (d *Dispatcher) enqueueRetry(agentName, taskID string, attempt int) {
	if d.rdb == nil {
		return
	}
	entry := retryEntry{TaskID: taskID, AgentName: agentName, Attempt: attempt + 1}
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[dispatcher] marshal retry entry: %v", err)
		return
	}
	key := retryQueuePrefix + agentName
	if err := d.rdb.RPush(context.Background(), key, data).Err(); err != nil {
		log.Printf("[dispatcher] failed to enqueue retry for agent %s task %s: %v", agentName, taskID, err)
	}
}

// processRetryQueue continuously pops entries from Redis retry lists and retries them.
func (d *Dispatcher) processRetryQueue(ctx context.Context) {
	defer d.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if d.rdb == nil {
				continue
			}
			d.processAllRetryQueues(ctx)
		}
	}
}

// processAllRetryQueues iterates over retry queues and retries pending entries.
func (d *Dispatcher) processAllRetryQueues(ctx context.Context) {
	iter := d.rdb.Scan(ctx, 0, retryQueuePrefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		for {
			data, err := d.rdb.LPop(ctx, key).Result()
			if err != nil {
				break
			}
			d.retryEntry(data)
		}
	}
}

// retryEntry parses a retry entry and re-dispatches or gives up.
func (d *Dispatcher) retryEntry(data string) {
	var entry retryEntry
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		log.Printf("[dispatcher] invalid retry entry %q: %v", data, err)
		return
	}

	if entry.Attempt >= maxRetries {
		log.Printf("[dispatcher] max retries exceeded for task %s to agent %s, giving up", entry.TaskID, entry.AgentName)
		return
	}

	task, err := d.taskRepo.GetByID(entry.TaskID)
	if err != nil || task == nil {
		log.Printf("[dispatcher] task %s not found, skipping retry", entry.TaskID)
		return
	}
	// Only retry tasks still pending or claimed
	if task.Status != "pending" && task.Status != "claimed" {
		return
	}

	agent, err := d.agentRepo.GetByName(entry.AgentName)
	if err != nil || agent == nil || agent.WebhookURL == "" {
		log.Printf("[dispatcher] agent %s not found or no webhook, skipping retry", entry.AgentName)
		return
	}

	log.Printf("[dispatcher] retry %d/%d for task %s to agent %s", entry.Attempt, maxRetries, entry.TaskID, entry.AgentName)
	d.sendWebhook(*agent, task, entry.Attempt)
}

// FindNextForAgent returns the best-matching pending task for the given agent name.
// Used by the polling endpoint GET /tasks/dispatch?agent=<name>.
func FindNextForAgent(agentRepo *db.AgentRepo, taskRepo *db.TaskRepo, agentName string) (*models.Task, error) {
	agent, err := agentRepo.GetByName(agentName)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	if agent == nil {
		return nil, nil
	}

	tasks, err := taskRepo.List("pending", "")
	if err != nil {
		return nil, fmt.Errorf("list pending tasks: %w", err)
	}
	if len(tasks) == 0 {
		return nil, nil
	}

	var matching []models.Task
	for _, t := range tasks {
		if len(t.RequiredSkills) == 0 {
			matching = append(matching, t)
			continue
		}
		ok := true
		for _, req := range t.RequiredSkills {
			found := false
			for _, s := range agent.Skills {
				if s == req {
					found = true
					break
				}
			}
			if !found {
				ok = false
				break
			}
		}
		if ok {
			matching = append(matching, t)
		}
	}

	if len(matching) == 0 {
		return nil, nil
	}

	// Return highest priority (lowest number)
	best := matching[0]
	for _, t := range matching[1:] {
		if t.Priority < best.Priority {
			best = t
		}
	}
	return &best, nil
}