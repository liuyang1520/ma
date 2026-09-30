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

func TestFragmentedLinkInput(t *testing.T) {
	var d Decoder
	var got []Event
	input := []byte("\t\x1b[Z\x1b[1;3D\x1b[1;3C\x1b[?1016;2$y\x1b[<0;123;456M\x1b[<0;123;456m\x1b[<20;2;3M\x1b[<32;3;4M\x1b[<68;2;3M")
	for _, b := range input {
		got = append(got, d.Feed([]byte{b})...)
	}
	want := []Event{
		{Key: "tab", Text: "\t"}, {Key: "backtab"}, {Key: "back"}, {Key: "forward"}, {Key: "mousemode", A: 2},
		{Key: "mouse", Mouse: &Mouse{X: 123, Y: 456}}, {Key: "mouse", Mouse: &Mouse{X: 123, Y: 456, Released: true}},
		{Key: "mouse", Mouse: &Mouse{X: 2, Y: 3, Modifiers: 20}}, {Key: "mouse", Mouse: &Mouse{X: 3, Y: 4, Motion: true}}, {Key: "wheelup"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if events := d.Feed([]byte("\x1b[<128;2;3M")); len(events) != 1 || events[0].Mouse.Button == 0 {
		t.Fatal("extended mouse button decoded as left click", events)
	}
	for _, bad := range []string{"\x1b[<0;0;3M", "\x1b[<-1;2;3M", "\x1b[<0;2;3;4M", "\x1b[<99999999999999999999999999;2;3M"} {
		if events := d.Feed([]byte(bad)); len(events) != 0 {
			t.Fatalf("invalid mouse sequence accepted: %q", bad)
		}
	}
}
