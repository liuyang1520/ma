package terminal

import (
	"reflect"
	"testing"
)

func TestFragmentedInput(t *testing.T) {
	var d Decoder
	var got []Event
	input := []byte("j\x1b[A\x1b[6~\x1b[6;40;20t\x1b_Gi=42;OK\x1b\\\x1b[<64;2;3M日本\x03")
	for _, b := range input {
		got = append(got, d.Feed([]byte{b})...)
	}
	want := []Event{{Key: "j", Text: "j"}, {Key: "up"}, {Key: "pagedown"}, {Key: "size6", A: 40, B: 20}, {Key: "graphics", Text: "Gi=42;OK"}, {Key: "wheelup"}, {Key: "日", Text: "日"}, {Key: "本", Text: "本"}, {Key: "quit", Text: "\x03"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
