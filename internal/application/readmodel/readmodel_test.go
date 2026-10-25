package readmodel

import "testing"

func TestItemStateValid(t *testing.T) {
	if !ItemActive.Valid() || !ItemDiscontinued.Valid() || ItemState("").Valid() || ItemState("GONE").Valid() {
		t.Fatal("only ACTIVE and DISCONTINUED are valid")
	}
}
