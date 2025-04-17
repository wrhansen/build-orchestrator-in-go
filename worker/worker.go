package worker

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-collections/collections/queue"
	"github.com/wrhansen/build-orchestrator-in-go/stats"
	"github.com/wrhansen/build-orchestrator-in-go/store"
	"github.com/wrhansen/build-orchestrator-in-go/task"
)

var logger *log.Logger

func init() {
	logger = log.New(os.Stdout, "[worker] ", log.Ldate|log.Ltime)
}

type Worker struct {
	Name      string
	Queue     queue.Queue
	Db        store.Store
	TaskCount int
	Stats     *stats.Stats
}

func (w *Worker) GetTasks() []*task.Task {
	taskList, err := w.Db.List()
	if err != nil {
		logger.Printf("Error getting tasks from db: %v\n", err)
		return nil
	}
	return taskList.([]*task.Task)
}

func (w *Worker) CollectStats() {
	for {
		logger.Println("Collecting stats")
		w.Stats = stats.GetStats()
		w.Stats.TaskCount = w.TaskCount
		time.Sleep(15 * time.Second)
	}
}

func (w *Worker) runTask() task.DockerResult {
	t := w.Queue.Dequeue()
	if t == nil {
		logger.Println("No tasks in the queue")
		return task.DockerResult{Error: nil}
	}

	taskQueued := t.(task.Task)
	fmt.Printf("Found task in queue: %v:\n", taskQueued)

	err := w.Db.Put(taskQueued.ID.String(), &taskQueued)
	if err != nil {
		msg := fmt.Errorf("error storing task %s: %v", taskQueued.ID.String(), err)
		logger.Printf("%s\n", msg)
		return task.DockerResult{Error: msg}
	}

	result, err := w.Db.Get(taskQueued.ID.String())
	if err != nil {
		msg := fmt.Errorf("error getting task %s from database: %v", taskQueued.ID.String(), err)
		logger.Println(msg)
		return task.DockerResult{Error: msg}
	}

	taskPersisted := *result.(*task.Task)
	if taskPersisted.State == task.Completed {
		return w.StopTask(taskPersisted)
	}

	var dockerResult task.DockerResult
	if task.ValidStateTransition(taskPersisted.State, taskQueued.State) {
		switch taskQueued.State {
		case task.Scheduled:
			if taskQueued.ContainerId != "" {
				dockerResult = w.StopTask(taskQueued)
				if dockerResult.Error != nil {
					logger.Printf("%v\n", dockerResult.Error)
				}
			}
			dockerResult = w.StartTask(taskQueued)
		// case task.Completed:  // Do I still need this?
		// 	dockerResult = w.StopTask(taskQueued)
		default:
			fmt.Printf("This is a mistake. taskPersisted: %v, taskQueued: %v\n", taskPersisted, taskQueued)
			dockerResult.Error = errors.New("we should not get here")
		}
	} else {
		err := fmt.Errorf("invalid transition from %v to %v", taskPersisted.State, taskQueued.State)
		dockerResult.Error = err
		return dockerResult
	}
	return dockerResult
}

func (w *Worker) AddTask(t task.Task) {
	w.Queue.Enqueue(t)
}

func (w *Worker) StartTask(t task.Task) task.DockerResult {
	t.StartTime = time.Now().UTC()
	config := task.NewConfig(&t)
	d := task.NewDocker(config)
	result := d.Run()
	if result.Error != nil {
		logger.Printf("Err running task %v: %v\n", t.ID, result.Error)
		t.State = task.Failed
		w.Db.Put(t.ID.String(), &t)
		return result
	}

	t.ContainerId = result.ContainerId
	t.State = task.Running
	w.Db.Put(t.ID.String(), &t)

	return result
}

func (w *Worker) StopTask(t task.Task) task.DockerResult {
	config := task.NewConfig(&t)
	d := task.NewDocker(config)

	stopResult := d.Stop(t.ContainerId)
	if stopResult.Error != nil {
		logger.Printf("%v\n", stopResult.Error)
	}

	t.FinishTime = time.Now().UTC()
	t.State = task.Completed
	w.Db.Put(t.ID.String(), &t)
	logger.Printf("Stopped and removed container %v for task %v\n", t.ContainerId, t.ID)

	return stopResult
}

func (w *Worker) RunTasks() {
	for {
		if w.Queue.Len() != 0 {
			result := w.runTask()
			if result.Error != nil {
				logger.Printf("Error running task: %v\n", result.Error)
			}
		} else {
			logger.Printf("No tasks to process currently.\n")
		}
		logger.Println("Sleeping for 10 seconds.")
		time.Sleep(10 * time.Second)
	}
}

func (w *Worker) InspectTask(t task.Task) task.DockerInspectResponse {
	config := task.NewConfig(&t)
	d := task.NewDocker(config)
	return d.Inspect(t.ContainerId)
}

func (w *Worker) UpdateTasks() {
	for {
		logger.Println("Checking status of tasks")
		w.updateTasks()
		logger.Println("Task updates completed")
		logger.Println("Sleeping for 15 seconds")
		time.Sleep(15 * time.Second)
	}
}

func (w *Worker) updateTasks() {
	tasks, err := w.Db.List()
	if err != nil {
		logger.Printf("Error getting tasks from db: %v\n", err)
		return
	}
	for _, t := range tasks.([]*task.Task) {
		if t.State == task.Running {
			resp := w.InspectTask(*t)
			if resp.Error != nil {
				fmt.Printf("ERROR: %v\n", resp.Error)
			}

			if resp.Container == nil {
				logger.Printf("No container for running task %s\n", t.ID)
				t.State = task.Failed
				w.Db.Put(t.ID.String(), t)
			}

			if resp.Container.State.Status == "exited" {
				logger.Printf("Container for task %s in non-running state %s", t.ID, resp.Container.State.Status)
				t.State = task.Failed
				w.Db.Put(t.ID.String(), t)
			}
			t.HostPorts = resp.Container.NetworkSettings.NetworkSettingsBase.Ports
			w.Db.Put(t.ID.String(), t)
		}
	}
}

func New(name string, taskDbType string) *Worker {
	w := Worker{
		Name:  name,
		Queue: *queue.New(),
	}

	var s store.Store
	var err error
	switch taskDbType {
	case "memory":
		s = store.NewInMemoryTaskStore()
	case "persistent":
		filename := fmt.Sprintf("%s_tasks.db", name)
		s, err = store.NewTaskStore(filename, 0600, "tasks")
	}
	if err != nil {
		logger.Printf("unable to create new task store: %v", err)
	}
	w.Db = s
	return &w
}
