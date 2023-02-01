package mr

import (
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/rpc"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// for sorting by key.
type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

// main/mrworker.go calls this function.
func Worker(mapf func(string, string) []KeyValue, reducef func(string, []string) string) {

	// Your worker implementation here.
	for {
		reply := askTask()
		switch reply.TaskType {
		case "map":
			mapfunction(mapf, &reply)
		case "reduce":
			reducefunction(reducef, &reply)
		case "noTask":
			time.Sleep(3 * time.Second)
		case "finished":
			return
		}

	}

}

func askTask() TaskReply {
	args := TaskArgs{}
	args.Pid = os.Getpid()
	reply := TaskReply{}

	ok := call("Coordinator.AskTask", &args, &reply)
	if !ok {
		log.Fatal("Call Coordinator.AskTask failed")
	}

	return reply
}

func mapfunction(mapf func(string, string) []KeyValue, reply *TaskReply) {
	filename := reply.Filename
	mapId := reply.MapId
	nReduce := reply.NReduce

	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("cannot open %v", filename)
	}
	content, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("cannot read %v", filename)
	}
	file.Close()

	kva := mapf(filename, string(content))

	wd, _ := os.Getwd()
	filepath := wd + "/"
	tmpfiles := make([]*os.File, nReduce)
	tmpkvstrs := make([]strings.Builder, nReduce)

	for i := 0; i < nReduce; i++ {
		tmpfiles[i], err = ioutil.TempFile(filepath, "mr-"+strconv.Itoa(mapId)+"-*.txt")
		if err != nil {
			log.Fatal("Tempfile created failed!")
		}
	}

	for _, kv := range kva {
		tmpkvstrs[ihash(kv.Key)%nReduce].WriteString(kv.Key + " " + kv.Value + "\n")
	}
	for i := 0; i < nReduce; i++ {
		_, err = tmpfiles[i].WriteString(tmpkvstrs[i].String())
		if err != nil {
			log.Fatal("Write tempfile failed!")
		}
	}

	for i := 0; i < 10; i++ {
		tmpfiles[i].Close()
		err = os.Rename(tmpfiles[i].Name(), filepath+"mr-"+strconv.Itoa(mapId)+"-"+strconv.Itoa(i)+".txt")
		if err != nil {
			log.Fatal("Rename tempfile failed!", err.Error())
		}
	}

	mapDone(mapId)

	defer func() {
		for i := 0; i < nReduce; i++ {
			tmpfiles[i].Close()
			os.Remove(tmpfiles[i].Name())
		}
	}()
}

func mapDone(mapId int) error {
	args := TaskArgs{}
	args.MapId = mapId

	ok := call("Coordinator.MapTaskDone", &args, nil)
	if !ok {
		log.Fatal("Call Coordinator.MapTaskDone failed")
	}

	return nil
}

func reducefunction(reducef func(string, []string) string, reply *TaskReply) {
	nMap := reply.NMap
	reduceId := reply.ReduceId

	wd, _ := os.Getwd()
	filepath := wd + "/"
	intermediate := []KeyValue{}
	var kv KeyValue

	for i := 0; i < nMap; i++ {
		tmpname := filepath + "mr-" + strconv.Itoa(i) + "-" + strconv.Itoa(reduceId) + ".txt"
		ss, _ := ioutil.ReadFile(tmpname)
		strs := strings.Split(string(ss), "\n")
		for ii := 0; ii < len(strs); ii++ {
			if strs[ii] == "" {
				break
			}
			tmpKV := strings.Split(strs[ii], " ")
			kv.Key = tmpKV[0]
			kv.Value = tmpKV[1]

			intermediate = append(intermediate, kv)
		}
	}

	sort.Sort(ByKey(intermediate))

	oname := filepath + "mr-out-" + strconv.Itoa(reduceId) + ".txt"
	ofile, _ := os.Create(oname)

	for i := 0; i < len(intermediate); {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := reducef(intermediate[i].Key, values)

		// this is the correct format for each line of Reduce output.
		fmt.Fprintf(ofile, "%v %v\n", intermediate[i].Key, output)

		i = j
	}

	for i := 0; i < nMap; i++ {
		tmpname := filepath + "mr-" + strconv.Itoa(i) + "-" + strconv.Itoa(reduceId) + ".txt"
		os.Remove(tmpname)
	}
	reduceDone(reduceId)

	defer ofile.Close()
}

func reduceDone(reduceId int) error {
	args := TaskArgs{}
	args.ReduceId = reduceId

	ok := call("Coordinator.ReduceTaskDone", &args, nil)
	if !ok {
		log.Fatal("Call Coordinator.ReduceTaskDone failed")
	}

	return nil
}

// example function to show how to make an RPC call to the coordinator.
//
// the RPC argument and reply types are defined in rpc.go.
func CallExample() {

	// declare an argument structure.
	args := ExampleArgs{}

	// fill in the argument(s).
	args.X = 99

	// declare a reply structure.
	reply := ExampleReply{}

	// send the RPC request, wait for the reply.
	// the "Coordinator.Example" tells the
	// receiving server that we'd like to call
	// the Example() method of struct Coordinator.
	ok := call("Coordinator.Example", &args, &reply)
	if ok {
		// reply.Y should be 100.
		fmt.Printf("reply.Y %v\n", reply.Y)
	} else {
		fmt.Printf("call failed!\n")
	}
}

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
