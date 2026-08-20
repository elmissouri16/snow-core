package process

import "testing"

func TestOutputReadPreservesSplitUTF8AcrossCursors(t *testing.T) {
	ring := newOutputRing(1024)
	if _, err := ring.Write([]byte("abcéZ")); err != nil {
		t.Fatal(err)
	}
	first, err := ring.read(nil, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.data) != "abc" || first.next != 3 {
		t.Fatalf("first read data=%q next=%d", first.data, first.next)
	}
	cursor := first.next
	second, err := ring.read(&cursor, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(second.data) != "éZ" || second.next != 6 {
		t.Fatalf("second read data=%q next=%d", second.data, second.next)
	}
}

func TestOutputReadRetainsIncompleteRunningRune(t *testing.T) {
	ring := newOutputRing(1024)
	if _, err := ring.Write([]byte{0xc3}); err != nil {
		t.Fatal(err)
	}
	first, err := ring.read(nil, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.data) != 0 || first.next != 0 {
		t.Fatalf("incomplete read data=%x next=%d", first.data, first.next)
	}
	if _, err := ring.Write([]byte{0xa9}); err != nil {
		t.Fatal(err)
	}
	second, err := ring.read(nil, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(second.data) != "é" || second.next != 2 {
		t.Fatalf("complete read data=%q next=%d", second.data, second.next)
	}
}
