package raft

import (
	"fmt"
	"testing"
	"time"
)

func TestTimer(t *testing.T) {
	tt := time.NewTimer(time.Second)
	fmt.Println((time.Now().UnixNano() / 1e6) % 100000)
	<-tt.C
	fmt.Println(time.Now().UnixNano() / 1e6)
	time.Sleep(time.Second * 2)
	fmt.Println(time.Now().UnixNano() / 1e6)
}
