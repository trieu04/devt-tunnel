package redact

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestRedact(t *testing.T) {
	r := New(map[string]string{
		"token": "abcd", "long": "abcdef", "inner": "bcde",
		"z-duplicate": "same-value", "a-duplicate": "same-value",
		"empty": "", "short": "abc", "tiny": "x",
	})
	for _, test := range []struct{ input, want string }{
		{"abcdef abcd bcde", "[secret:long] [secret:token] [secret:inner]"},
		{"same-valuesame-value", "[secret:a-duplicate][secret:a-duplicate]"},
		{"abcdabcd abcdefabcdef", "[secret:token][secret:token] [secret:long][secret:long]"},
		{"nothing abc x", "nothing abc x"},
		{"abc", "abc"},
		{"", ""},
	} {
		if got := r.Redact(test.input); got != test.want {
			t.Errorf("Redact(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	wantIgnored := []IgnoredSecret{{"empty", IgnoreEmpty}, {"short", IgnoreShort}, {"tiny", IgnoreShort}}
	if got := r.Ignored(); !reflect.DeepEqual(got, wantIgnored) {
		t.Fatalf("Ignored = %#v, want %#v", got, wantIgnored)
	}
	copy := r.Ignored()
	copy[0].Name = "changed"
	if reflect.DeepEqual(copy, r.Ignored()) {
		t.Fatal("Ignored returned mutable internal state")
	}
}

func push(t *testing.T, s *Stream, seq uint64, text string) []Line {
	t.Helper()
	lines, err := s.Push(Line{Seq: seq, Line: text})
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestStreamSplitAcrossWritesAndLines(t *testing.T) {
	s := New(map[string]string{"api": "super-secret-token"}).NewStream()
	if got := push(t, s, 812, "header super-sec"); len(got) != 0 {
		t.Fatalf("leaked a pending source record: %#v", got)
	}
	got := push(t, s, 813, "ret-token tail!")
	want := []Line{{812, "header [secret:api]"}, {813, " tail!"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if got := s.Close(); len(got) != 0 {
		t.Fatalf("unexpected trailing records: %#v", got)
	}
}

func TestStreamEverySplitAndSingleByteWrites(t *testing.T) {
	r := New(map[string]string{"short": "abcd", "long": "abcdef", "other": "xyzy"})
	text := "prefix abcdef abcd xyzyabcdef suffix!"
	want := r.Redact(text)
	for width := 1; width <= len(text); width++ {
		s := r.NewStream()
		var got strings.Builder
		var last uint64
		var count int
		for offset, seq := 0, uint64(1); offset < len(text); seq++ {
			end := offset + width
			if end > len(text) {
				end = len(text)
			}
			for _, line := range push(t, s, seq, text[offset:end]) {
				if line.Seq != last+1 {
					t.Fatalf("width %d: sequence %d after %d", width, line.Seq, last)
				}
				last, count = line.Seq, count+1
				got.WriteString(line.Line)
			}
			offset = end
		}
		for _, line := range s.Close() {
			last, count = line.Seq, count+1
			got.WriteString(line.Line)
		}
		if got.String() != want {
			t.Fatalf("width %d: got %q, want %q", width, got.String(), want)
		}
		if count != (len(text)+width-1)/width || uint64(count) != last {
			t.Fatalf("width %d: lost source records (%d, last %d)", width, count, last)
		}
	}
}

func TestStreamWaitsForLongestMatch(t *testing.T) {
	r := New(map[string]string{"short": "abcd", "long": "abcdef"})
	s := r.NewStream()
	if got := push(t, s, 1, "abcd"); len(got) != 0 {
		t.Fatalf("emitted shorter match prematurely: %#v", got)
	}
	if got, want := push(t, s, 2, "ef!"), []Line{{1, "[secret:long]"}, {2, "!"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	s = r.NewStream()
	push(t, s, 1, "abcd")
	if got, want := s.Close(), []Line{{1, "[secret:short]"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EOF preferred an incomplete longer match: %#v", got)
	}
	s = r.NewStream()
	push(t, s, 1, "abcd")
	if got, want := push(t, s, 2, "X"), []Line{{1, "[secret:short]"}, {2, "X"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestStreamFlushAndCloseMaskUnresolvedPrefix(t *testing.T) {
	for _, closeStream := range []bool{false, true} {
		s := New(map[string]string{"token": "abcdefgh"}).NewStream()
		push(t, s, 7, "innocent abc")
		push(t, s, 8, "")
		var got []Line
		if closeStream {
			got = s.Close()
		} else {
			// A caller's idle-timeout handler invokes Flush.
			got = s.Flush()
		}
		want := []Line{{7, "innocent [secret:token]"}, {8, ""}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("close=%v: got %#v, want %#v", closeStream, got, want)
		}
		if got := s.Flush(); len(got) != 0 {
			t.Fatalf("second flush = %#v", got)
		}
		if closeStream {
			if _, err := s.Push(Line{9, "x"}); !errors.Is(err, ErrClosed) {
				t.Fatalf("Push after Close = %v", err)
			}
		} else if got, want := push(t, s, 9, "defgh!"), []Line{{9, "defgh!"}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("post-timeout match boundary: %#v", got)
		}
		if got := s.Close(); len(got) != 0 {
			t.Fatalf("repeated close = %#v", got)
		}
	}
}

func TestSequenceRejectionDoesNotAcceptInput(t *testing.T) {
	s := New(map[string]string{"token": "abcdef"}).NewStream()
	push(t, s, 10, "abc")
	for _, seq := range []uint64{10, 9, 12} {
		if _, err := s.Push(Line{seq, "unsafe"}); !errors.Is(err, ErrSequence) {
			t.Fatalf("seq %d: %v", seq, err)
		}
	}
	if got, want := push(t, s, 11, "def!"), []Line{{10, "[secret:token]"}, {11, "!"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("state changed on rejection: %#v", got)
	}
	s = New(nil).NewStream()
	push(t, s, ^uint64(0), "last")
	if _, err := s.Push(Line{0, "wrapped"}); !errors.Is(err, ErrSequence) {
		t.Fatalf("sequence wrap = %v", err)
	}
}

func TestNoSecretsEmptyLinesAndNoReplacementRescan(t *testing.T) {
	s := New(nil).NewStream()
	for seq, text := range []string{"", "plain", ""} {
		want := []Line{{uint64(seq), text}}
		if got := push(t, s, uint64(seq), text); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
	r := New(map[string]string{"token": "actual-secret", "marker": "[secret:token]"})
	if got := r.Redact("actual-secret"); got != "[secret:token]" {
		t.Fatalf("replacement was rescanned: %q", got)
	}
}

// NOT CAUGHT: unknown values; base64/URL-encoded, hashed or case-transformed
// values; partial echo; input omitted by a retained-log gap; and values spanning
// an explicit timeout/close boundary. This is exact matching, not a DLP system.
func TestDocumentedLimitations(t *testing.T) {
	r := New(map[string]string{"token": "s3cr/et+value"})
	for _, text := range []string{
		"unknown-sensitive-value", "czNjci9ldCt2YWx1ZQ==", "s3cr%2Fet%2Bvalue",
		"S3CR/ET+VALUE", "s3cr/et", "et+value",
	} {
		if got := r.Redact(text); got != text {
			t.Fatalf("unexpected detection of transformed/partial value %q: %q", text, got)
		}
	}
}

func TestConcurrentRedactorUse(t *testing.T) {
	r := New(map[string]string{"token": "secret-value"})
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if got := r.Redact("secret-value!"); got != "[secret:token]!" {
					t.Errorf("got %q", got)
				}
				s := r.NewStream()
				s.Push(Line{1, "secret-"})
				s.Push(Line{2, "value!"})
				s.Close()
			}
		}()
	}
	wg.Wait()
}
