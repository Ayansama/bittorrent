package torrent

import (
	"testing"
)

func TestDecodeInteger(t *testing.T) {
	val, _, err := Decode([]byte("i42e"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if val.(int64) != 42 {
		t.Fatalf("expected 42 got %v", val)
	}
}

func TestDecodeString(t *testing.T) {
	val, _, err := Decode([]byte("4:spam"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(val.([]byte)) != "spam" {
		t.Fatalf("expected spam got %v", val)
	}
}

func TestDecodeList(t *testing.T) {
	val, _, err := Decode([]byte("l4:spam3:fooe"), 0)
	if err != nil {
		t.Fatal(err)
	}
	list := val.([]interface{})
	if len(list) != 2 {
		t.Fatalf("expected 2 items got %d", len(list))
	}
	if string(list[0].([]byte)) != "spam" {
		t.Fatalf("expected spam got %v", list[0])
	}
}

func TestDecodeDict(t *testing.T) {
	val, _, err := Decode([]byte("d3:cow3:moo4:spam4:eggse"), 0)
	if err != nil {
		t.Fatal(err)
	}
	dict := val.(map[string]interface{})
	if string(dict["cow"].([]byte)) != "moo" {
		t.Fatalf("expected moo got %v", dict["cow"])
	}
}