package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	//	"bytes"

	"bytes"
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	//	"6.824/labgob"
	"6.824/labgob"
	"6.824/labrpc"
	"golang.org/x/sync/semaphore"
)

const (
	FOLLOWER = iota
	PREVOTE
	CANDIDATE
	LEADER
)

const Dev = false

const heartsBeatTime = time.Millisecond * 100
const checkMatchIndexInterval = time.Millisecond * 40
const InstallSnapshotTimeout = time.Millisecond * 1000

// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For 2D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

type llog struct {
	Term    int
	Command interface{}
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	state    uint8
	votedNum int
	applyCh  chan ApplyMsg
	peerswg  sync.WaitGroup

	leaderAlive bool
	aeTimer     *time.Timer

	timer                    *time.Timer
	timeOut                  time.Duration
	sendAppendEntriesTimeout []time.Duration
	sendRequestVoteTimeout   []time.Duration

	currentTerm  int
	votedFor     int
	log          []llog
	logBaseIndex int

	commitIndex      int
	lastApplied      int
	applierSemaphore *semaphore.Weighted

	nextIndex  []int
	matchIndex []int

	snapshot []byte
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool

	// Your code here (2A).
	isleader = false
	rf.mu.Lock()
	term = rf.currentTerm
	if rf.state == LEADER {
		isleader = true
	}
	rf.mu.Unlock()

	return term, isleader
}

type PersistentSate struct {
	CurrentTerm       int
	VoteFor           int
	Log               []llog
	LastIncludedIndex int
	LastIncludedTerm  int
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	tmpPersist := PersistentSate{
		CurrentTerm:       rf.currentTerm,
		VoteFor:           rf.votedFor,
		Log:               rf.log,
		LastIncludedIndex: rf.logBaseIndex,
		LastIncludedTerm:  rf.log[0].Term,
	}
	e.Encode(tmpPersist)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}
func (rf *Raft) persistStateAndSnapshot(snapshot []byte) {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	tmpPersist := PersistentSate{
		CurrentTerm:       rf.currentTerm,
		VoteFor:           rf.votedFor,
		Log:               rf.log,
		LastIncludedIndex: rf.logBaseIndex,
		LastIncludedTerm:  rf.log[0].Term,
	}
	e.Encode(tmpPersist)
	data := w.Bytes()
	rf.persister.SaveStateAndSnapshot(data, snapshot)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var tmpPersist PersistentSate
	if d.Decode(&tmpPersist) != nil {
		fmt.Println("Error: Decode Failed!")
	} else {
		rf.print("readPersist: PersistentState,", tmpPersist)
		rf.currentTerm = tmpPersist.CurrentTerm
		rf.votedFor = tmpPersist.VoteFor
		rf.log = tmpPersist.Log
		rf.logBaseIndex = tmpPersist.LastIncludedIndex
		rf.commitIndex = tmpPersist.LastIncludedIndex
		rf.lastApplied = tmpPersist.LastIncludedIndex
		rf.log[0].Term = tmpPersist.LastIncludedTerm
	}
}

// A service wants to switch to snapshot.  Only do so if Raft hasn't
// have more recent info since it communicate the snapshot on applyCh.
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {

	// Your code here (2D).

	return true
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).
	if index > rf.getLastIndex() || rf.logBaseIndex >= index {
		return
	}
	rf.print("+++++++++++++++++++++++++logLength:", rf.getLogLength(), len(rf.log), "index:", index)

	rf.log[0].Term = rf.getLogByIndex(index).Term
	rf.log = append(rf.log[:1], rf.log[index+1-rf.logBaseIndex:]...)
	rf.logBaseIndex = index
	rf.snapshot = snapshot

	wState := new(bytes.Buffer)
	eState := labgob.NewEncoder(wState)
	tmpPersist := PersistentSate{
		CurrentTerm:       rf.currentTerm,
		VoteFor:           rf.votedFor,
		Log:               rf.log,
		LastIncludedIndex: rf.logBaseIndex,
		LastIncludedTerm:  rf.log[0].Term,
	}
	eState.Encode(tmpPersist)
	dataState := wState.Bytes()

	rf.print("-------------------------logLength:", rf.getLogLength(), len(rf.log), "index:", index)
	rf.persister.SaveStateAndSnapshot(dataState, snapshot)
}

type InstallSnapshotArgs struct {
	Term              int
	LeaderId          int
	LastIncludedIndex int
	LastIncludedTerm  int
	Data              []byte
}

type InstallSnapshotReply struct {
	Term int
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persistStateAndSnapshot(args.Data)
	reply.Term = rf.currentTerm
	if args.Term >= rf.currentTerm {
		rf.print("received peer", args.LeaderId, "InstallSnapshot")
		if args.Term > rf.currentTerm {
			rf.print("finds Term", args.Term, "peer", args.LeaderId, "is leader")
			rf.print(rf.stateStr(), "-> Follower ")
			rf.currentTerm = args.Term
			rf.state = FOLLOWER
			rf.votedFor = args.LeaderId
			rf.votedNum = 0
			rf.resetTimer()
		}
		rf.print("args.LastIndex:", args.LastIncludedIndex, "rf.logBaseIndex:", rf.logBaseIndex, "log:", rf.log)
		index := args.LastIncludedIndex
		if rf.logBaseIndex < index {
			if index <= rf.getLastIndex() {
				rf.log = append(rf.log[:1], rf.log[index+1-rf.logBaseIndex:]...)
			} else {
				rf.log = rf.log[:1]
			}
			rf.logBaseIndex = index
			rf.log[0].Term = args.LastIncludedTerm
			rf.commitIndex = max(index, rf.commitIndex)
			rf.lastApplied = max(index, rf.lastApplied)

			rf.print("::::::::::::begin InstallSnapshot...")
			// rf.mu.Unlock()
			rf.applyCh <- ApplyMsg{CommandValid: false, SnapshotValid: true, Snapshot: args.Data, SnapshotIndex: index, SnapshotTerm: args.LastIncludedTerm}
			// rf.mu.Lock()
			rf.print("::::::::::::InstallSnapshot complete!")
		}
	}
}

func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	tt := time.NewTimer(InstallSnapshotTimeout)
	done := make(chan bool)

	go func() {
		ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
		done <- ok
	}()

	select {
	case <-tt.C:
		rf.print("Call peer", server, "SendInstallSnapshot Timeout!")
		return false
	case ok := <-done:
		return ok
	}
}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
	IsPreVote    bool
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	reply.VoteGranted = false

	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()
	reply.Term = rf.currentTerm
	if args.IsPreVote {
		rf.print("received peer", args.CandidateId, "RequestPreVote", rf.leaderAlive, args.LastLogIndex, rf.getLastIndex())
		if !rf.leaderAlive {
			if args.LastLogTerm > rf.getLastLog().Term || (args.LastLogTerm == rf.getLastLog().Term && args.LastLogIndex >= rf.getLastIndex()) {
				rf.print("prevotes for peer", str(args.CandidateId)+"/"+str(rf.votedFor))
				reply.VoteGranted = true
			}
		}
	} else {
		rf.print("received peer", args.CandidateId, "RequestVote")
		if args.Term >= rf.currentTerm {
			if rf.state == FOLLOWER {
				rf.timer.Reset(rf.timeOut)
			}

			if args.Term > rf.currentTerm {
				rf.print("finds peer", args.CandidateId, "Term", args.Term, "larger then its currentTerm", rf.currentTerm)
				rf.print(rf.stateStr(), "-> Follower ")
				rf.currentTerm = args.Term
				rf.state = FOLLOWER
				rf.votedFor = -1
				rf.votedNum = 0
				rf.resetTimer()
			}

			if args.LastLogTerm > rf.getLastLog().Term || (args.LastLogTerm == rf.getLastLog().Term && args.LastLogIndex >= rf.getLastIndex()) {
				if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
					rf.print("votes for peer", str(args.CandidateId)+"/"+str(rf.votedFor))
					rf.votedFor = args.CandidateId
					reply.VoteGranted = true
				} else {
					rf.print("votes for", rf.votedFor, "doesn't vote for peer", args.CandidateId)
				}
			} else {
				rf.print("--------", args.LastLogIndex, rf.getLastIndex(), "--------------")
				rf.print("doesn't vote for peer", args.CandidateId)
			}
		}
	}
}

// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	tt := time.NewTimer(rf.sendRequestVoteTimeout[server])
	t1 := time.Now()
	done := make(chan bool)

	go func() {
		ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
		done <- ok
	}()

	select {
	case <-tt.C:
		rf.mu.Lock()
		if rf.sendRequestVoteTimeout[server]*2 > heartsBeatTime {
			rf.sendRequestVoteTimeout[server] = heartsBeatTime
		} else {
			rf.sendRequestVoteTimeout[server] *= 2
		}
		rf.print("Call peer", server, "SendRequestVote Timeout!")
		rf.mu.Unlock()
		return false
	case ok := <-done:
		rf.mu.Lock()
		if ok {
			t2 := time.Since(t1)
			rf.sendRequestVoteTimeout[server] = time.Duration(float64(rf.sendRequestVoteTimeout[server])*0.8 + float64(t2)*0.2)
		}
		rf.mu.Unlock()
		return ok
	}
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []llog
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool

	XTerm  int
	XIndex int
	XLen   int
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.print("received peer", args.LeaderId, "RequestAppendEntries")
	reply.Success = false

	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()
	reply.Term = rf.currentTerm
	if args.Term >= rf.currentTerm {
		if rf.state == FOLLOWER {
			rf.timer.Reset(rf.timeOut)
			rf.print("rf.timer reset", rf.timeOut)
		} else {
			rf.print("finds Term", args.Term, "peer", args.LeaderId, "is Leader")
			rf.currentTerm = args.Term
			rf.state = FOLLOWER
			rf.votedFor = args.LeaderId
			rf.votedNum = 0
			rf.resetTimer()
		}
		rf.leaderAlive = true
		rf.aeTimer.Reset(rf.timeOut)

		ttt := false
		if args.PrevLogIndex == -1 {
			ttt = true
		} else {
			if args.PrevLogIndex > rf.getLastIndex() {
				reply.XTerm = -1
				reply.XIndex = -1
				reply.XLen = rf.getLogLength()
			} else {
				rf.print("args.PrevLogIndex:", args.PrevLogIndex)
				if rf.getLogByIndex(args.PrevLogIndex).Term != args.PrevLogTerm {
					rf.print(">>>>>>>>>", rf.log, "<<<<<<<<<")
					rf.print(">>>>>>>>>Index:", args.PrevLogIndex, "Term:", args.PrevLogTerm, "<<<<<<<<<")
					tmpTerm := rf.getLogByIndex(args.PrevLogIndex).Term
					for i := args.PrevLogIndex - 1; i > 0; i-- {
						if tmpTerm != rf.getLogByIndex(i).Term && tmpTerm < args.PrevLogTerm {
							reply.XIndex = i
							break
						}
					}
					reply.XTerm = tmpTerm
					reply.XLen = rf.getLogLength()
					rf.print(">>>>>>>>>XIndex:", reply.XIndex, "XTerm:", reply.XTerm, "XLen:", reply.XLen, "<<<<<<<<<")
				} else {
					ttt = true
				}
			}
		}

		if ttt {
			rf.print("log length is", rf.getLogLength(), "args.PrevLogIndex from peer", args.LeaderId, "is", args.PrevLogIndex)
			reply.Success = true

			if len(args.Entries) > 0 {
				rf.log = rf.log[:args.PrevLogIndex+1-rf.logBaseIndex]
				rf.log = append(rf.log, args.Entries...)
				rf.print(rf.log, args.Entries)
			}
			if rf.commitIndex < args.LeaderCommit {
				rf.setCommitIndex(min(args.LeaderCommit, rf.getLastIndex()))
			}
		}
	}
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	tt := time.NewTimer(rf.sendAppendEntriesTimeout[server])
	t1 := time.Now()
	done := make(chan bool)

	go func() {
		ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
		done <- ok
	}()

	select {
	case <-tt.C:
		rf.mu.Lock()
		if rf.sendAppendEntriesTimeout[server]*2 > heartsBeatTime {
			rf.sendAppendEntriesTimeout[server] = heartsBeatTime
		} else {
			rf.sendAppendEntriesTimeout[server] *= 2
		}
		rf.print("Call peer", server, "SendAppendEntries Timeout!")
		rf.mu.Unlock()
		return false
	case ok := <-done:
		rf.mu.Lock()
		if ok {
			t2 := time.Since(t1)
			rf.sendAppendEntriesTimeout[server] = time.Duration(float64(rf.sendAppendEntriesTimeout[server])*0.8 + float64(t2)*0.2)
		}
		rf.mu.Unlock()
		return ok
	}
}

func (rf *Raft) sendAppendEntriesToAll() {
	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue
		}

		prevLogIndex := rf.nextIndex[i] - 1
		rf.print("send", i, "request", prevLogIndex, rf.logBaseIndex)
		if prevLogIndex < rf.logBaseIndex {
			args := InstallSnapshotArgs{
				Term:              rf.currentTerm,
				LeaderId:          rf.me,
				LastIncludedIndex: rf.logBaseIndex,
				LastIncludedTerm:  rf.log[0].Term,
				Data:              rf.snapshot,
			}
			go func(server int) {
				reply := InstallSnapshotReply{}
				ok := rf.sendInstallSnapshot(server, &args, &reply)
				if !ok {
					rf.print("Call peer", server, "SendInstallSnapshot Failed!")
					return
				}

				rf.mu.Lock()
				defer rf.mu.Unlock()
				if reply.Term > rf.currentTerm {
					rf.state = FOLLOWER
					rf.currentTerm = reply.Term
					rf.print("Leader -> Follower")
					rf.persist()
				} else {
					rf.nextIndex[server] = rf.getLastIndex() + 1
					rf.matchIndex[server] = rf.logBaseIndex
				}
			}(i)
		} else {
			prevLogTerm := rf.getLogByIndex(prevLogIndex).Term
			nextIndex := rf.getLastIndex() + 1
			entries := make([]llog, rf.getLastIndex()-prevLogIndex)
			copy(entries, rf.log[prevLogIndex+1-rf.logBaseIndex:])
			args := AppendEntriesArgs{
				Term:         rf.currentTerm,
				LeaderId:     rf.me,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				LeaderCommit: rf.commitIndex,
				Entries:      entries,
			}

			go func(server int) {
				reply := AppendEntriesReply{}

				ok := rf.sendAppendEntries(server, &args, &reply)
				if !ok {
					rf.print("Call peer", server, "SendAppendEntries Failed!")
					return
				}

				rf.mu.Lock()
				defer rf.mu.Unlock()
				if reply.Success {
					rf.nextIndex[server] = nextIndex
					rf.matchIndex[server] = nextIndex - 1
				} else {
					if reply.Term > rf.currentTerm {
						rf.state = FOLLOWER
						rf.currentTerm = reply.Term
						rf.print("Leader -> Follower")
						rf.persist()
					} else {
						if reply.XTerm == -1 {
							rf.nextIndex[server] = reply.XLen
						} else {
							// rf.nextIndex[server] -= 1
							lastLogInXterm := rf.getLastIndexBySameTerm(reply.XTerm)
							if lastLogInXterm >= 0 {
								rf.nextIndex[server] = lastLogInXterm
							} else {
								rf.nextIndex[server] = reply.XIndex + 1
							}
						}
					}
				}
			}(i)
		}
	}
}

// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).
	rf.mu.Lock()
	if rf.state != LEADER {
		isLeader = false
	} else {
		term = rf.currentTerm
		rf.log = append(rf.log, llog{Term: term, Command: command})
		index = rf.getLastIndex()
		rf.matchIndex[rf.me] = index
		rf.print("received command", command)
		rf.print("append log index is", index)
		rf.persist()
	}
	rf.mu.Unlock()

	return index, term, isLeader
}

// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// The ticker go routine starts a new election if this peer hasn't received
// heartsbeats recently.
func (rf *Raft) ticker() {
	for !rf.killed() {

		// Your code here to check if a leader election should
		// be started and to randomize sleeping time using
		rf.mu.Lock()
		switch rf.state {
		case FOLLOWER:
			rf.mu.Unlock()
			<-rf.timer.C

			rf.mu.Lock()
			rf.state = PREVOTE
			rf.print("Follower -> PreVote")
			rf.persist()
			rf.mu.Unlock()

		case PREVOTE:
			rf.votedNum = 1
			rf.mu.Unlock()

			rf.peerswg.Add(len(rf.peers) - 1)
			for i := 0; i < len(rf.peers); i++ {
				if i == rf.me {
					continue
				}
				go func(i int) {
					defer rf.peerswg.Done()

					rf.mu.Lock()
					args := RequestVoteArgs{
						Term:         rf.currentTerm,
						CandidateId:  rf.me,
						LastLogIndex: rf.getLastIndex(),
						LastLogTerm:  rf.getLastLog().Term,
						IsPreVote:    true,
					}
					reply := RequestVoteReply{}
					rf.mu.Unlock()

					ok := rf.sendRequestVote(i, &args, &reply)
					if !ok {
						rf.print("Call peer", i, "SendRequestVote Failed!")
						return
					}
					rf.mu.Lock()
					defer rf.mu.Unlock()
					if reply.VoteGranted {
						rf.votedNum += 1
					}
				}(i)
			}
			rf.peerswg.Wait()

			rf.mu.Lock()
			if rf.state != PREVOTE {
				rf.mu.Unlock()
				continue
			}
			if rf.votedNum > len(rf.peers)/2 {
				rf.print("PreVote -> Candidate", str(len(rf.peers))+"/"+str(rf.votedNum))
				rf.state = CANDIDATE
				rf.persist()
				rf.mu.Unlock()
				continue
			}
			rf.mu.Unlock()
			rf.voteSleep()

		case CANDIDATE:
			rf.currentTerm += 1
			rf.votedFor = rf.me
			rf.votedNum = 1
			rf.persist()
			rf.mu.Unlock()

			termStale := -1
			rf.peerswg.Add(len(rf.peers) - 1)
			for i := 0; i < len(rf.peers); i++ {
				if i == rf.me {
					continue
				}
				go func(i int) {
					defer rf.peerswg.Done()

					rf.mu.Lock()
					args := RequestVoteArgs{
						Term:         rf.currentTerm,
						CandidateId:  rf.me,
						LastLogIndex: rf.getLastIndex(),
						LastLogTerm:  rf.getLastLog().Term,
						IsPreVote:    false,
					}
					reply := RequestVoteReply{}
					rf.mu.Unlock()

					ok := rf.sendRequestVote(i, &args, &reply)
					if !ok {
						rf.print("Call peer", i, "SendRequestVote Failed!")
						return
					}

					rf.mu.Lock()
					defer rf.mu.Unlock()
					if reply.VoteGranted {
						rf.votedNum += 1
					} else if reply.Term > rf.currentTerm {
						termStale = reply.Term
					}
				}(i)
			}
			rf.peerswg.Wait()

			rf.mu.Lock()
			if rf.state != CANDIDATE {
				rf.mu.Unlock()
				continue
			}
			if termStale > 0 {
				rf.currentTerm = termStale
				rf.state = FOLLOWER
				rf.print("Candidate -> Follower")
				rf.persist()
				rf.mu.Unlock()
				continue
			}
			if rf.votedNum > len(rf.peers)/2 {
				rf.state = LEADER
				rf.print("Candidate -> Leader,", str(len(rf.peers))+"/"+str(rf.votedNum))
				for i := 0; i < len(rf.nextIndex); i++ {
					rf.nextIndex[i] = rf.getLastIndex() + 1
				}
				go rf.checkMatchIndexFunc()
				rf.persist()
				rf.mu.Unlock()
				continue
			}
			rf.votedFor = -1
			rf.persist()
			rf.mu.Unlock()
			rf.voteSleep()

		case LEADER:
			rf.leaderAlive = true
			rf.sendAppendEntriesToAll()
			rf.mu.Unlock()

			rf.mu.Lock()
			if rf.state != LEADER {
				rf.mu.Unlock()
				continue
			}
			rf.print("leader waiting")
			rf.mu.Unlock()
			time.Sleep(heartsBeatTime)
		}
	}
}

func (rf *Raft) applier() {
	for !rf.killed() {
		rf.applierSemaphore.Acquire(context.Background(), 1)
		rf.lastApplied++
		log := rf.getLogByIndex(rf.lastApplied)
		rf.print("log", rf.lastApplied, log, "-> applyCh")
		rf.applyCh <- ApplyMsg{CommandValid: true, CommandIndex: rf.lastApplied, Command: log.Command}
	}
}

func (rf *Raft) checkMatchIndexFunc() {
	for !rf.killed() {
		rf.mu.Lock()
		if rf.state != LEADER {
			rf.mu.Unlock()
			break
		}
		if rf.commitIndex+1 < rf.getLogLength() {
			majorityOfMatchIndex := rf.majorityOfMatchIndex()
			rf.print(rf.matchIndex)
			if rf.logBaseIndex < majorityOfMatchIndex && rf.getLogByIndex(majorityOfMatchIndex).Term == rf.currentTerm {
				rf.print(rf.commitIndex, rf.getLogLength(), majorityOfMatchIndex, rf.matchIndex)
				rf.setCommitIndex(majorityOfMatchIndex)
				rf.nextIndex[rf.me] = rf.commitIndex + 1
				rf.print("commitIndex", rf.commitIndex, "nextIndex", rf.nextIndex)
				rf.sendAppendEntriesToAll()
			}
		}
		rf.mu.Unlock()
		time.Sleep(checkMatchIndexInterval)
	}
}

// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.state = FOLLOWER
	rand.Seed(time.Now().UnixNano())
	rf.timeOut = time.Millisecond * time.Duration(rand.Intn(150)+250)
	rf.sendRequestVoteTimeout = make([]time.Duration, len(rf.peers))
	rf.sendAppendEntriesTimeout = make([]time.Duration, len(rf.peers))
	for i := 0; i < len(rf.peers); i++ {
		rf.sendRequestVoteTimeout[i] = time.Millisecond * 40
		rf.sendAppendEntriesTimeout[i] = time.Millisecond * 40
	}
	rf.timer = time.NewTimer(rf.timeOut)
	rf.applyCh = applyCh

	rf.leaderAlive = false
	rf.aeTimer = time.NewTimer(rf.timeOut)
	go func() {
		for !rf.killed() {
			<-rf.aeTimer.C
			rf.mu.Lock()
			rf.leaderAlive = false
			rf.aeTimer.Reset(heartsBeatTime * 2)
			rf.print("rf.aeTimer reset")
			rf.mu.Unlock()
		}
	}()

	rf.currentTerm = 0
	rf.votedFor = -1
	rf.log = make([]llog, 1, 1024)
	rf.logBaseIndex = 0

	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.applierSemaphore = semaphore.NewWeighted(1024)
	rf.applierSemaphore.Acquire(context.Background(), 1024)

	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))
	for i := 0; i < len(rf.peers); i++ {
		rf.nextIndex[i] = 1
		rf.matchIndex[i] = 0
	}

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()
	go rf.applier()

	return rf
}

func (rf *Raft) setCommitIndex(commitIndex int) {
	rf.print("Release:", commitIndex-rf.commitIndex)
	rf.applierSemaphore.Release(int64(commitIndex - rf.commitIndex))
	rf.commitIndex = commitIndex
}

func (rf *Raft) getLogByIndex(index int) llog {
	return rf.log[index-rf.logBaseIndex]
}

func (rf *Raft) getLogLength() int {
	return rf.logBaseIndex + len(rf.log)
}

func (rf *Raft) getLastLog() llog {
	return rf.getLogByIndex(rf.getLogLength() - 1)
}

func (rf *Raft) getFirstIndex() int {
	return rf.logBaseIndex + 1
}

func (rf *Raft) getLastIndex() int {
	return rf.getLogLength() - 1
}

func (rf *Raft) getLastIndexBySameTerm(XTerm int) int {
	lastLogInXTerm := -1
	for i := rf.getFirstIndex(); i < rf.getLogLength(); i++ {
		if rf.getLogByIndex(i).Term > XTerm {
			if rf.getLogByIndex(i-1).Term == XTerm {
				lastLogInXTerm = i - 1
			}
			break
		}
	}
	return lastLogInXTerm
}

func (rf *Raft) print(a ...interface{}) {
	if Dev {
		a = append([]interface{}{
			interface{}((time.Now().UnixNano() / 1e6) % 100000),
			interface{}("t"),
			interface{}(rf.currentTerm),
			interface{}("c"),
			interface{}(rf.commitIndex),
			interface{}("p"),
			interface{}(rf.me),
		},
			a...)
		fmt.Println(a...)
	}
}

func (rf *Raft) resetTimer() {
	rand.Seed(time.Now().UnixNano())
	rf.timeOut = time.Millisecond * time.Duration(rand.Intn(150)+250)
	rf.timer.Reset(rf.timeOut)
}

func (rf *Raft) voteSleep() {
	time.Sleep(time.Millisecond * time.Duration(rand.Intn(150)+250))
}

func (rf *Raft) stateStr() string {
	switch rf.state {
	case FOLLOWER:
		return "Follower"
	case PREVOTE:
		return "PreVote"
	case CANDIDATE:
		return "Candidate"
	case LEADER:
		return "Leader"
	}
	return "Unknown State"
}

func (rf *Raft) majorityOfMatchIndex() int {
	tt := make([]int, len(rf.matchIndex))
	copy(tt, rf.matchIndex)
	sort.Slice(tt, func(i, j int) bool {
		return tt[i] > tt[j]
	})
	return tt[len(rf.matchIndex)/2]
}

func str(i int) string {
	return strconv.Itoa(i)
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}
