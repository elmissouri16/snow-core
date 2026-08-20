package session

import (
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func testSubagentRecord() SubagentRecord {
	return SubagentRecord{State: protocol.SubagentState{Agent: protocol.AgentRef{ThreadID: "child", ParentThreadID: "root", Path: "/root/child", ParentPath: "/root", Role: "explorer", Depth: 1}, Status: protocol.AgentQueued, Model: "m", Provider: "p", Thinking: protocol.ThinkingOff, CreatedAt: 1, Generation: 1}, ParentBranchID: "main", ChildSessionPath: "root.db.agents/child.db"}
}

func TestMemorySubagentTaskStoreCAS(t *testing.T) {
	st := NewMemoryStore(Options{})
	rec := testSubagentRecord()
	if err := st.PutSubagent(rec); err != nil {
		t.Fatal(err)
	}
	next := rec
	next.State.Status = protocol.AgentRunning
	next.State.Generation = 2
	if err := st.CompareAndSwapSubagent("child", 1, next); err != nil {
		t.Fatal(err)
	}
	if err := st.CompareAndSwapSubagent("child", 1, next); err == nil {
		t.Fatal("stale CAS accepted")
	}
	list, _ := st.ListSubagents()
	if len(list) != 1 || list[0].State.Status != protocol.AgentRunning {
		t.Fatalf("list=%+v", list)
	}
}

func TestSQLiteSubagentTopologyPersistsAndMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root.db")
	st, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec := testSubagentRecord()
	if err := st.PutSubagent(rec); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	re, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	list, err := re.ListSubagents()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ChildSessionPath != rec.ChildSessionPath || list[0].State.Agent.Path != "/root/child" {
		t.Fatalf("list=%+v", list)
	}
}
