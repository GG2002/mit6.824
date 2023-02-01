package mr

import (
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"
)

type Coordinator struct {
	// Your definitions here.
	nMap             int
	mapTasks         chan mapTask
	mapFinishedTasks map[int]bool
	mapFinishedNum   int
	mapMutex         sync.Mutex

	nReduce             int
	reduceTasks         chan reduceTask
	reduceFinishedTasks map[int]bool
	reduceFinishedNum   int
	reduceMutex         sync.Mutex

	finished bool
}

type mapTask struct {
	pid   int
	state string

	mapId    int
	fileName string
}

type reduceTask struct {
	pid   int
	state string

	reduceId int
}

// Your code here -- RPC handlers for the worker to call.
func (c *Coordinator) AskTask(args *TaskArgs, reply *TaskReply) error {
	reply.NReduce = c.nReduce
	reply.NMap = c.nMap
	reply.TaskType = "noTask"

	maptask, mapOK := <-c.mapTasks
	if mapOK {
		maptask.pid = args.Pid
		reply.TaskType = "map"
		reply.Filename = maptask.fileName
		reply.MapId = maptask.mapId

		go func() {
			time.Sleep(time.Second * 10)
			c.mapMutex.Lock()
			if !c.mapFinishedTasks[maptask.mapId] {
				maptask.pid = 0
				c.mapTasks <- maptask
			}
			c.mapMutex.Unlock()
		}()

		// fmt.Println("map", maptask.mapId, maptask.pid)
		return nil
	}

	reducetask, reduceOK := <-c.reduceTasks
	if reduceOK {
		reducetask.pid = args.Pid
		reply.TaskType = "reduce"
		reply.ReduceId = reducetask.reduceId

		go func() {
			time.Sleep(time.Second * 10)
			c.reduceMutex.Lock()
			if !c.reduceFinishedTasks[reducetask.reduceId] {
				reducetask.pid = 0
				c.reduceTasks <- reducetask
			}
			c.reduceMutex.Unlock()
		}()

		// fmt.Println("reduce", reducetask.reduceId, reducetask.pid)
		return nil
	}

	reply.TaskType = "finished"

	return nil
}

func (c *Coordinator) MapTaskDone(args *TaskArgs, reply *TaskReply) error {
	c.mapMutex.Lock()

	if !c.mapFinishedTasks[args.MapId] {
		c.mapFinishedTasks[args.MapId] = true
		c.mapFinishedNum++

		if c.mapFinishedNum == c.nMap {
			close(c.mapTasks)
		}
	}

	c.mapMutex.Unlock()

	return nil
}

func (c *Coordinator) ReduceTaskDone(args *TaskArgs, reply *TaskReply) error {
	c.reduceMutex.Lock()

	if !c.reduceFinishedTasks[args.ReduceId] {
		c.reduceFinishedTasks[args.ReduceId] = true
		c.reduceFinishedNum++

		if c.reduceFinishedNum == c.nReduce {
			close(c.reduceTasks)
			c.finished = true
		}
	}

	c.reduceMutex.Unlock()

	return nil
}

// start a thread that listens for RPCs from worker.go
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := coordinatorSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
func (c *Coordinator) Done(args *TaskArgs, reply *TaskReply) error {
	reply.State = "F"

	// Your code here.
	c.reduceMutex.Lock()
	c.mapMutex.Lock()
	// fmt.Println(c.mapFinishedTasks, c.reduceFinishedTasks)
	if c.finished {
		reply.State = "T"
	}
	c.mapMutex.Unlock()
	c.reduceMutex.Unlock()

	return nil
}

// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{}

	// Your code here.
	c.finished = false

	c.nMap = len(files)
	c.mapFinishedNum = 0
	c.mapTasks = make(chan mapTask, c.nMap)
	c.mapFinishedTasks = make(map[int]bool, c.nMap)

	c.nReduce = nReduce
	c.reduceFinishedNum = 0
	c.reduceTasks = make(chan reduceTask, c.nReduce)
	c.reduceFinishedTasks = make(map[int]bool, c.nReduce)

	for i := 0; i < len(files); i++ {
		c.mapTasks <- mapTask{state: "map", pid: 0, fileName: files[i], mapId: i}
	}

	for i := 0; i < c.nReduce; i++ {
		c.reduceTasks <- reduceTask{state: "reduce", pid: 0, reduceId: i}
	}

	c.server()
	return &c
}
