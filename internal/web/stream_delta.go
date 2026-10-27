package web

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// streamDelta remembers the message and activity encodings a single SSE
// connection has already delivered. Later frames replace every entry whose
// encoding is byte-identical with a stub {"id":…,"unchanged":true}, so a frame
// costs the size of what changed rather than the whole bounded history. The
// browser merges stubs from its previous frame and reconnects on any gap; a
// fresh connection always starts from a complete frame.
type streamDelta struct {
	messages   map[string][]byte
	activities map[string][]byte
}

// streamSnapshotFrame overrides the two repeated collections with pre-encoded
// entries while every other public field keeps its ordinary encoding.
type streamSnapshotFrame struct {
	RuntimeSnapshot
	Messages   []jsontext.Value `json:"messages"`
	Activities []jsontext.Value `json:"activities"`
}

func (d *streamDelta) frame(snapshot RuntimeSnapshot) (streamSnapshotFrame, error) {
	frame := streamSnapshotFrame{RuntimeSnapshot: snapshot}
	var err error
	frame.Messages, d.messages, err = encodeDelta(d.messages, len(snapshot.Messages), func(i int) (string, any) {
		return snapshot.Messages[i].ID, snapshot.Messages[i]
	})
	if err != nil {
		return frame, err
	}
	frame.Activities, d.activities, err = encodeDelta(d.activities, len(snapshot.Activities), func(i int) (string, any) {
		return snapshot.Activities[i].ID, snapshot.Activities[i]
	})
	return frame, err
}

func encodeDelta(previous map[string][]byte, count int, entry func(int) (string, any)) ([]jsontext.Value, map[string][]byte, error) {
	encoded := make([]jsontext.Value, 0, count)
	next := make(map[string][]byte, count)
	for i := range count {
		id, value := entry(i)
		data, err := json.Marshal(value)
		if err != nil {
			return nil, nil, err
		}
		// Only an unambiguous identity may stand in for content.
		_, duplicate := next[id]
		if id != "" && !duplicate {
			next[id] = data
			if last, ok := previous[id]; ok && bytes.Equal(last, data) {
				stub, err := json.Marshal(struct {
					ID        string `json:"id"`
					Unchanged bool   `json:"unchanged"`
				}{ID: id, Unchanged: true})
				if err != nil {
					return nil, nil, err
				}
				encoded = append(encoded, jsontext.Value(stub))
				continue
			}
		}
		encoded = append(encoded, jsontext.Value(data))
	}
	return encoded, next, nil
}
