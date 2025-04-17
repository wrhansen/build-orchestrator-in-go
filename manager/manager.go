package manager

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/golang-collections/collections/queue"
	"github.com/google/uuid"
	"github.com/wrhansen/build-orchestrator-in-go/node"
	"github.com/wrhansen/build-orchestrator-in-go/scheduler"
	"github.com/wrhansen/build-orchestrator-in-go/store"
	"github.com/wrhansen/build-orchestrator-in-go/task"
	"github.com/wrhansen/build-orchestrator-in-go/worker"
)

var logger *log.Logger

func init() {
	logger = log.New(os.Stdout, "[manager] ", log.Ldate|log.Ltime)
}

type Manager struct {
	Pending       queue.Queue
	TaskDb        store.Store
	EventDb       store.Store
	Workers       []string
	WorkerTaskMap map[string][]uuid.UUID
	TaskWorkerMap map[uuid.UUID]string
	LastWorker    int
	WorkerNodes   []*node.Node
	Scheduler     scheduler.Scheduler
}

func (m *Manager) SelectWorker(t task.Task) (*node.Node, error) {
	candidates := m.Scheduler.SelectCandidateNodes(t, m.WorkerNodes)
	if candidates == nil {
		msg := fmt.Sprintf("No available candidates match resource request for task %v", t.ID)
		err := errors.New(msg)
		return nil, err
	}
	scores := m.Scheduler.Score(t, candidates)
	selectedNode := m.Scheduler.Pick(scores, candidates)
	return selectedNode, nil
}

func (m *Manager) updateTasks() {
	for _, worker := range m.Workers {
		logger.Printf("Checking worker %v for task updates", worker)
		url := fmt.Sprintf("http://%s/tasks", worker)
		resp, err := http.Get(url)
		if err != nil {
			logger.Printf("Error connecting to %v: %v\n", worker, err)
		}
		if resp.StatusCode != http.StatusOK {
			logger.Printf("Error sending request: %v\n", err)
		}

		d := json.NewDecoder(resp.Body)
		var tasks []*task.Task
		err = d.Decode(&tasks)
		if err != nil {
			logger.Printf("Error unmarshalling tasks: %s\n", err.Error())
		}

		for _, t := range tasks {
			logger.Printf("Attempting to update task %v\n", t.ID)

			result, err := m.TaskDb.Get(t.ID.String())
			if err != nil {
				logger.Printf("%s\n", err)
				continue
			}
			taskPersisted, ok := result.(*task.Task)
			if !ok {
				logger.Printf("cannot convert result %v to task.Task type\n", result)
				continue
			}

			if taskPersisted.State != t.State {
				taskPersisted.State = t.State
			}

			taskPersisted.StartTime = t.StartTime
			taskPersisted.FinishTime = t.FinishTime
			taskPersisted.ContainerId = t.ContainerId
			taskPersisted.HostPorts = t.HostPorts

			m.TaskDb.Put(t.ID.String(), taskPersisted)
		}
	}
}

func (m *Manager) SendWork() {
	if m.Pending.Len() > 0 {
		e := m.Pending.Dequeue()
		te := e.(task.TaskEvent)
		err := m.EventDb.Put(te.ID.String(), &te)
		if err != nil {
			logger.Printf("error attempting to store task event %s: %s\n", te.ID.String(), err)
			return
		}
		logger.Printf("Pulled %v off pending queue\n", te)

		taskWorker, ok := m.TaskWorkerMap[te.Task.ID]
		if ok {
			result, err := m.TaskDb.Get(te.Task.ID.String())
			if err != nil {
				logger.Printf("unable to schedule task: %s", err)
				return
			}

			persistedTask, ok := result.(*task.Task)
			if !ok {
				logger.Printf("unable to convert task to task.Task type\n")
				return
			}

			if te.State == task.Completed && task.ValidStateTransition(persistedTask.State, te.State) {
				m.stopTask(taskWorker, te.Task.ID.String())
				return
			}

			logger.Printf("invalid request: existing task %s is in state %v and cannot transition to the completed state\n", persistedTask.ID.String(), persistedTask.State)
			return
		}

		t := te.Task
		w, err := m.SelectWorker(t)
		if err != nil {
			logger.Printf("error selecting worker for task %s: %v\n", t.ID, err)
		}

		logger.Printf("selected worker %s for task %s\n", w.Name, t.ID)

		m.WorkerTaskMap[w.Name] = append(m.WorkerTaskMap[w.Name], te.Task.ID)
		m.TaskWorkerMap[t.ID] = w.Name

		t.State = task.Scheduled
		m.TaskDb.Put(t.ID.String(), &t)

		data, err := json.Marshal(te)
		if err != nil {
			logger.Printf("Unable to marshal task object: %v.\n", t)
		}

		// send task to worker
		url := fmt.Sprintf("http://%s/tasks", w.Name)
		resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
		if err != nil {
			logger.Printf("Error connecting to %v: %v\n", w, err)
			m.Pending.Enqueue(te)
			return
		}

		d := json.NewDecoder(resp.Body)
		if resp.StatusCode != http.StatusCreated {
			e := worker.ErrResponse{}
			err := d.Decode(&e)
			if err != nil {
				fmt.Printf("Error decoding response: %v\n", err.Error())
				return
			}
			logger.Printf("Response error (%d): %s", e.HTTPStatusCode, e.Message)
			return
		}
		t = task.Task{}
		err = d.Decode(&t)
		if err != nil {
			fmt.Printf("Error decoding response: %s\n", err.Error())
			return
		}
		w.TaskCount++
		logger.Printf("received response from worker: %#v\n", t)
	} else {
		logger.Println("No work in the queue")
	}
}

func (m *Manager) AddTask(te task.TaskEvent) {
	logger.Printf("Add event %v to pending queue", te)
	m.Pending.Enqueue(te)
}

func (m *Manager) GetTasks() []*task.Task {
	taskList, err := m.TaskDb.List()
	if err != nil {
		logger.Printf("error getting list of tasks: %v\n", err)
		return nil
	}

	return taskList.([]*task.Task)
}

func (m *Manager) UpdateTasks() {
	for {
		logger.Println("Checking for task updates from workers")
		m.updateTasks()
		logger.Println("Task updates completed")
		logger.Println("Sleeping for 15 seconds")
		time.Sleep(15 * time.Second)
	}
}

func (m *Manager) ProcessTasks() {
	for {
		logger.Println("Processing any tasks in the queue")
		m.SendWork()
		logger.Println("Sleeping for 10 seconds")
		time.Sleep(10 * time.Second)
	}
}

func (m *Manager) checkTaskHealth(t task.Task) error {
	logger.Printf("Calling health check for task %s: %s\n", t.ID, t.HealthCheck)

	w := m.TaskWorkerMap[t.ID]
	hostPort := getHostPort(t.HostPorts)
	worker := strings.Split(w, ":")
	if hostPort == nil {
		logger.Printf("Have not collected task %s host port yet. Skipping.\n", t.ID)
		return nil
	}
	url := fmt.Sprintf("http://%s:%s%s", worker[0], *hostPort, t.HealthCheck)

	logger.Printf("Calling health check for task %s: %s\n", t.ID, url)
	resp, err := http.Get(url)
	if err != nil {
		msg := fmt.Sprintf("Error connecting to health check %s", url)
		logger.Println(msg)
		return errors.New(msg)
	}

	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("Error health check for task %s did not return 200\n", t.ID)
		logger.Println(msg)
		return errors.New(msg)
	}

	logger.Printf("Task %s health check response: %v\n", t.ID, resp.StatusCode)
	return nil
}

func getHostPort(ports nat.PortMap) *string {
	for k := range ports {
		return &ports[k][0].HostPort
	}
	return nil
}

func (m *Manager) doHealthChecks() {
	tasks := m.GetTasks()
	for _, t := range tasks {
		if t.State == task.Running && t.RestartCount < 3 {
			err := m.checkTaskHealth(*t)
			if err != nil {
				if t.RestartCount < 3 {
					m.restartTask(t)
				}
			}
		} else if t.State == task.Failed && t.RestartCount < 3 {
			m.restartTask(t)
		}
	}
}

func (m *Manager) restartTask(t *task.Task) {
	w := m.TaskWorkerMap[t.ID]
	t.State = task.Scheduled
	t.RestartCount++
	m.TaskDb.Put(t.ID.String(), t)

	te := task.TaskEvent{
		ID:        uuid.New(),
		State:     task.Running,
		Timestamp: time.Now(),
		Task:      *t,
	}
	data, err := json.Marshal(te)
	if err != nil {
		logger.Printf("Unable to marshal task object: %v.", t)
		return
	}

	url := fmt.Sprintf("http://%s/tasks", w)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		logger.Printf("Error connecting to %v: %v", w, err)
		m.Pending.Enqueue(t)
		return
	}

	d := json.NewDecoder(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		e := worker.ErrResponse{}
		err := d.Decode(&e)
		if err != nil {
			fmt.Printf("Error decoding response: %s\n", err.Error())
			return
		}
		logger.Printf("Response error (%d): %s", e.HTTPStatusCode, e.Message)
		return
	}

	newTask := task.Task{}
	err = d.Decode(&newTask)
	if err != nil {
		fmt.Printf("Error decoding response: %s\n", err.Error())
		return
	}
	logger.Printf("%#v\n", t)
}

// TODO: is this method really necessary since the scheduler is calling node.GetStats() itself?
func (m *Manager) UpdateNodeStats() {
	for {
		for _, node := range m.WorkerNodes {
			logger.Printf("Collecting stats for node %v", node.Name)
			_, err := node.GetStats()
			if err != nil {
				logger.Printf("error updating node stats: %v", err)
			}
		}
		time.Sleep(15 * time.Second)
	}
}

func (m *Manager) DoHealthChecks() {
	for {
		logger.Println("Performing task health check")
		m.doHealthChecks()
		logger.Println("Task health checks completed")
		logger.Println("Sleeping for 60 seconds")
		time.Sleep(60 * time.Second)
	}
}

func (m *Manager) stopTask(worker string, taskID string) {
	client := &http.Client{}
	url := fmt.Sprintf("http://%s/tasks/%s", worker, taskID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		logger.Printf("Error creating request to delete task %s: %v\n", taskID, err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Printf("error connecting to worker at %s: %v\n", url, err)
		return
	}

	if resp.StatusCode != 204 {
		logger.Printf("Error sending request: %v\n", err)
		return
	}

	logger.Printf("task %s has been scheduled to be stopped", taskID)
}

func New(workers []string, schedulerType string, dbType string) *Manager {
	workerTaskMap := make(map[string][]uuid.UUID)
	taskWorkerMap := make(map[uuid.UUID]string)

	var nodes []*node.Node
	for worker := range workers {
		workerTaskMap[workers[worker]] = []uuid.UUID{}

		nAPI := fmt.Sprintf("http://%v", workers[worker])
		n := node.NewNode(workers[worker], nAPI, "worker")
		nodes = append(nodes, n)
	}

	var s scheduler.Scheduler
	switch schedulerType {
	case "roundrobin":
		s = &scheduler.RoundRobin{Name: "roundrobin"}
	case "epvm":
		s = &scheduler.Epvm{Name: "epvm"}
	default:
		s = &scheduler.RoundRobin{Name: "roundrobin"}
	}

	m := Manager{
		Pending:       *queue.New(),
		Workers:       workers,
		WorkerTaskMap: workerTaskMap,
		TaskWorkerMap: taskWorkerMap,
		WorkerNodes:   nodes,
		Scheduler:     s,
	}

	var ts store.Store
	var es store.Store
	var err error
	switch dbType {
	case "memory":
		ts = store.NewInMemoryTaskStore()
		es = store.NewInMemoryTaskEventStore()
	case "persistent":
		ts, err = store.NewTaskStore("task.db", 0600, "tasks")
		es, err = store.NewEventStore("events.db", 0600, "events")
	}

	if err != nil {
		logger.Fatalf("unable to create task store: %v", err)
	}
	if err != nil {
		logger.Fatalf("unable to create task event store: %v", err)
	}

	m.TaskDb = ts
	m.EventDb = es

	return &m
}
