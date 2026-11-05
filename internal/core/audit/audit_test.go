package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var testTime = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func testEvent() Event {
	return Event{Actor: Actor{Kind: ActorMCP, Name: "coding-agent"}, Action: LeaseOpen,
		TunnelID: "tmp-staging-1", Decision: Allow, Result: ResultOK}
}

func openWriter(t *testing.T, dir string, options Options) *Writer {
	t.Helper()
	if options.Clock == nil {
		options.Clock = func() time.Time { return testTime }
	}
	w, err := Open(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return w
}

func appendEvent(t *testing.T, w *Writer, event Event) Event {
	t.Helper()
	written, err := w.Append(event)
	if err != nil {
		t.Fatal(err)
	}
	return written
}

func readBatch(t *testing.T, dir string, after uint64, filter Filter) Batch {
	t.Helper()
	batch, err := NewReader(dir).Read(after, filter)
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestAppendClockPermissionsAndRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	zone := time.FixedZone("local", 2*60*60)
	clock := func() time.Time { return testTime.In(zone) }
	w := openWriter(t, dir, Options{Clock: clock})
	input := testEvent()
	input.Sequence, input.Time = 999, time.Unix(1, 0)
	input.SecretNames = []string{"api-token"}
	written := appendEvent(t, w, input)
	if written.Sequence != 1 || written.Time != testTime || written.Time.Location() != time.UTC {
		t.Fatalf("clock/sequence not assigned: %#v", written)
	}
	input.SecretNames[0] = "changed"
	if written.SecretNames[0] != "api-token" {
		t.Fatal("Append retained caller's slice")
	}
	for path, mode := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, FileName): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("%s mode %o, want %o", path, info.Mode().Perm(), mode)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(testEvent()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Append after Close = %v", err)
	}
	// Existing file permissions are tightened, not just applied on creation.
	if err := os.Chmod(filepath.Join(dir, FileName), 0o644); err != nil {
		t.Fatal(err)
	}
	w = openWriter(t, dir, Options{})
	if got := appendEvent(t, w, testEvent()).Sequence; got != 2 {
		t.Fatalf("restarted sequence = %d", got)
	}
	batch := readBatch(t, dir, 0, Filter{})
	if len(batch.Events) != 2 || batch.Next != 2 || batch.Oldest != 1 || batch.Lost {
		t.Fatalf("batch = %#v", batch)
	}
	info, _ := os.Stat(filepath.Join(dir, FileName))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("existing file mode = %o", info.Mode().Perm())
	}
}

func TestRotationRetentionOffsetsAndRestart(t *testing.T) {
	dir := t.TempDir()
	w := openWriter(t, dir, Options{MaxBytes: 480, KeepFiles: 3})
	for i := 0; i < 18; i++ {
		appendEvent(t, w, testEvent())
	}
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("kept %d files, want active + 2 archives", len(list))
	}
	for _, name := range []string{FileName, FileName + ".1", FileName + ".2"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 480 || info.Size() == 0 || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: size %d, mode %o", name, info.Size(), info.Mode().Perm())
		}
	}
	all := readBatch(t, dir, 0, Filter{})
	if all.Next != 18 || all.Oldest <= 1 || all.Lost || len(all.Events) == 0 {
		t.Fatalf("retained batch = %#v", all)
	}
	for i, event := range all.Events {
		if event.Sequence != all.Oldest+uint64(i) {
			t.Fatalf("noncontiguous retained events: %#v", all.Events)
		}
	}
	lost := readBatch(t, dir, 1, Filter{})
	if !lost.Lost || !reflect.DeepEqual(lost.Events, all.Events) {
		t.Fatalf("expired offset not reported: %#v", lost)
	}
	fromMiddle := readBatch(t, dir, all.Oldest, Filter{})
	if !reflect.DeepEqual(fromMiddle.Events, all.Events[1:]) || fromMiddle.Lost {
		t.Fatalf("cross-rotation offset: %#v", fromMiddle)
	}
	if _, err := NewReader(dir).Read(19, Filter{}); !errors.Is(err, ErrCursorAhead) {
		t.Fatalf("ahead cursor = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = openWriter(t, dir, Options{MaxBytes: 480, KeepFiles: 3})
	if got := appendEvent(t, w, testEvent()).Sequence; got != 19 {
		t.Fatalf("sequence after rotated restart = %d", got)
	}
}

func TestReducedRetentionPrunesOldArchives(t *testing.T) {
	dir := t.TempDir()
	w := openWriter(t, dir, Options{MaxBytes: 480, KeepFiles: 6})
	for i := 0; i < 18; i++ {
		appendEvent(t, w, testEvent())
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = openWriter(t, dir, Options{MaxBytes: 480, KeepFiles: 2})
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("reduced retention kept %d files", len(list))
	}
	if got := appendEvent(t, w, testEvent()).Sequence; got != 19 {
		t.Fatalf("pruning reset sequence to %d", got)
	}
}

func TestKeepOneAndExactSizeBoundary(t *testing.T) {
	dir := t.TempDir()
	event := testEvent()
	event.Sequence, event.Time = 1, testTime
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	w := openWriter(t, dir, Options{MaxBytes: int64(len(data) + 1), KeepFiles: 1})
	appendEvent(t, w, testEvent())
	appendEvent(t, w, testEvent())
	batch := readBatch(t, dir, 0, Filter{})
	if len(batch.Events) != 1 || batch.Events[0].Sequence != 2 {
		t.Fatalf("keep one = %#v", batch)
	}
	list, _ := os.ReadDir(dir)
	if len(list) != 1 || list[0].Name() != FileName {
		t.Fatalf("keep one created archives: %#v", list)
	}
}

func TestConcurrentWritesAndReadersThroughRotation(t *testing.T) {
	dir := t.TempDir()
	w := openWriter(t, dir, Options{MaxBytes: 1600, KeepFiles: 32})
	const goroutines, writes = 8, 12
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writes; j++ {
				event := testEvent()
				event.Actor.Name = fmt.Sprintf("agent-%d", id)
				if _, err := w.Append(event); err != nil {
					t.Errorf("Append: %v", err)
					return
				}
			}
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	reader := NewReader(dir)
	var cursor uint64
	read := func() {
		batch, err := reader.Read(cursor, Filter{})
		if errors.Is(err, ErrBusy) {
			return
		}
		if err != nil {
			t.Errorf("concurrent Read: %v", err)
			return
		}
		if batch.Lost {
			t.Error("reader lost events despite sufficient retention")
		}
		for _, event := range batch.Events {
			if event.Sequence != cursor+1 {
				t.Errorf("got sequence %d after %d", event.Sequence, cursor)
			}
			cursor = event.Sequence
		}
		if batch.Next != cursor {
			t.Errorf("cursor %d != Next %d", cursor, batch.Next)
		}
	}
loop:
	for {
		read()
		select {
		case <-done:
			break loop
		default:
		}
	}
	read()
	if cursor != goroutines*writes {
		t.Fatalf("read %d events, want %d", cursor, goroutines*writes)
	}
	all := readBatch(t, dir, 0, Filter{})
	if len(all.Events) != goroutines*writes {
		t.Fatalf("retained %d events", len(all.Events))
	}
}

func TestFiltersAndFilteredOffsets(t *testing.T) {
	dir := t.TempDir()
	now := testTime
	w := openWriter(t, dir, Options{Clock: func() time.Time { return now }})
	for i := 0; i < 4; i++ {
		event := testEvent()
		if i%2 == 1 {
			event.Actor.Name = "other"
			event.TunnelID = "database"
		}
		now = testTime.Add(time.Duration(i) * time.Minute)
		appendEvent(t, w, event)
	}
	for _, test := range []struct {
		name   string
		filter Filter
		want   []uint64
	}{
		{"actor", Filter{Actor: "coding-agent"}, []uint64{1, 3}},
		{"tunnel", Filter{Tunnel: "database"}, []uint64{2, 4}},
		{"since-inclusive", Filter{Since: testTime.Add(2 * time.Minute)}, []uint64{3, 4}},
		{"combined", Filter{Actor: "coding-agent", Tunnel: "tmp-staging-1", Since: testTime.Add(time.Minute)}, []uint64{3}},
		{"exact-not-prefix", Filter{Actor: "coding"}, nil},
		{"no-match", Filter{Tunnel: "absent"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			batch := readBatch(t, dir, 0, test.filter)
			var got []uint64
			for _, event := range batch.Events {
				got = append(got, event.Sequence)
			}
			if !reflect.DeepEqual(got, test.want) || batch.Next != 4 {
				t.Fatalf("sequences %v, Next %d; want %v, 4", got, batch.Next, test.want)
			}
		})
	}
	// Clock rollback does not affect sequence-based following.
	now = testTime.Add(-time.Hour)
	appendEvent(t, w, testEvent())
	batch := readBatch(t, dir, 4, Filter{})
	if len(batch.Events) != 1 || batch.Events[0].Sequence != 5 {
		t.Fatalf("clock rollback skipped an event: %#v", batch)
	}
}

func TestFollowAcrossRotationAndCancellation(t *testing.T) {
	dir := t.TempDir()
	w := openWriter(t, dir, Options{MaxBytes: 480, KeepFiles: 3})
	appendEvent(t, w, testEvent())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got []uint64
	err := NewReader(dir).Follow(ctx, 0, Filter{}, time.Millisecond, func(batch Batch) error {
		if batch.Lost {
			t.Fatal("follow unexpectedly lost records")
		}
		for _, event := range batch.Events {
			got = append(got, event.Sequence)
		}
		if batch.Next == 12 {
			cancel()
		} else {
			appendEvent(t, w, testEvent()) // forces several rotations during follow
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || len(got) != 12 {
		t.Fatalf("Follow = %v; events %v", err, got)
	}
	for i, seq := range got {
		if seq != uint64(i+1) {
			t.Fatalf("duplicate/missing events: %v", got)
		}
	}
	stop := errors.New("output closed")
	if err := NewReader(dir).Follow(context.Background(), 0, Filter{}, 0, func(Batch) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("callback error = %v", err)
	}
}

func TestFullOpenApproveCloseFlowOmitsSecretsAndLogText(t *testing.T) {
	// Simulate real operation inputs, deliberately including sensitive argv,
	// purpose, process output and an error. The projection into Event is explicit;
	// the audit API cannot accept this request, its payload, argv or error.
	input := struct {
		Actor       Actor
		TunnelID    string
		SecretName  string
		SecretValue string
		LogLine     string
		Purpose     string
		Argv        []string
		Failure     error
	}{
		Actor: Actor{Kind: ActorMCP, Name: "coding-agent"}, TunnelID: "tmp-public-1",
		SecretName: "ngrok-personal", SecretValue: "KNOWN-SECRET-DO-NOT-RECORD-8da12",
		LogLine: "PRIVATE PROCESS OUTPUT must never be in audit files",
	}
	input.Argv = []string{"ngrok", "--authtoken=" + input.SecretValue}
	input.Purpose = "debug " + input.SecretValue + " " + input.LogLine
	input.Failure = fmt.Errorf("%s: %s", input.SecretValue, input.LogLine)
	fingerprint := sha256.Sum256([]byte("ngrok http 3000; secret_name=ngrok-personal"))
	base := Event{Actor: input.Actor, TunnelID: input.TunnelID, RequestID: "req-1",
		LeaseID: "ls-1", ApprovalID: "ap-1", Fingerprint: hex.EncodeToString(fingerprint[:]),
		SecretNames: []string{input.SecretName}}
	dir := t.TempDir()
	w := openWriter(t, dir, Options{MaxBytes: 600, KeepFiles: 8})
	flow := []struct {
		action   Action
		actor    Actor
		decision Decision
		result   Result
	}{
		{LeaseOpen, input.Actor, Ask, ResultPendingApproval},
		{ApprovalRequest, input.Actor, Ask, ResultPendingApproval},
		{ApprovalDecide, Actor{Kind: ActorGUI, Name: "owner"}, Allow, ResultOK},
		{LeaseOpen, input.Actor, Allow, ResultOK},
		{LeaseClose, input.Actor, Allow, ResultOK},
	}
	for _, step := range flow {
		event := base
		event.Action, event.Actor, event.Decision, event.Result = step.action, step.actor, step.decision, step.result
		if step.action == ApprovalDecide {
			event.ApprovalScope = ScopeOnce
		}
		appendEvent(t, w, event)
	}
	batch := readBatch(t, dir, 0, Filter{})
	if len(batch.Events) != len(flow) || batch.Next != uint64(len(flow)) {
		t.Fatalf("vacuous flow test, events = %#v", batch)
	}
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 {
		t.Fatal("flow did not exercise archives")
	}
	var combined []byte
	for _, file := range list {
		data, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{input.SecretValue, input.LogLine, input.Purpose, input.Failure.Error(), "authtoken"} {
			if bytes.Contains(data, []byte(forbidden)) {
				t.Fatalf("sensitive input leaked into %s", file.Name())
			}
		}
		combined = append(combined, data...)
	}
	if !bytes.Contains(combined, []byte(input.SecretName)) {
		t.Fatal("secret name reference was lost")
	}
}

func TestValidationRejectsFreeTextWithoutEchoingIt(t *testing.T) {
	dir := t.TempDir()
	w := openWriter(t, dir, Options{})
	private := "PRIVATE SECRET and log text\nextra line"
	for _, mutate := range []func(*Event){
		func(e *Event) { e.Action = Action(private) },
		func(e *Event) { e.Result = Result(private) },
		func(e *Event) { e.Decision = Decision(private) },
		func(e *Event) { e.ApprovalScope = ApprovalScope(private) },
		func(e *Event) { e.Actor.Kind = ActorKind(private) },
		func(e *Event) { e.Actor.Name = private },
		func(e *Event) { e.TunnelID = private },
		func(e *Event) { e.SecretNames = []string{private} },
		func(e *Event) { e.Fingerprint = private },
		func(e *Event) { e.Actor.Name = strings.Repeat("x", 257) },
		func(e *Event) { e.Actor.Name = string([]byte{0xff}) },
	} {
		event := testEvent()
		mutate(&event)
		if _, err := w.Append(event); !errors.Is(err, ErrInvalidEvent) || strings.Contains(err.Error(), private) {
			t.Fatalf("validation error = %v", err)
		}
	}
	if got := readBatch(t, dir, 0, Filter{}); len(got.Events) != 0 {
		t.Fatalf("invalid events written: %#v", got)
	}
	w2 := openWriter(t, t.TempDir(), Options{MaxBytes: 1})
	if _, err := w2.Append(testEvent()); !errors.Is(err, ErrEventTooLarge) {
		t.Fatalf("oversize event = %v", err)
	}
	// This allowlist intentionally fails if Event gains an unreviewed field.
	typ := reflect.TypeFor[Event]()
	wantFields := []string{"Sequence", "Time", "Actor", "Action", "TunnelID", "LeaseID", "RequestID", "ApprovalID", "Fingerprint", "SecretNames", "Decision", "ApprovalScope", "Result"}
	var fields []string
	for i := 0; i < typ.NumField(); i++ {
		fields = append(fields, typ.Field(i).Name)
	}
	if !reflect.DeepEqual(fields, wantFields) {
		t.Fatalf("audit schema changed: %v", fields)
	}
}

func TestIncompleteTailAndCorruption(t *testing.T) {
	dir := t.TempDir()
	w := openWriter(t, dir, Options{})
	appendEvent(t, w, testEvent())
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, FileName)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"seq":2`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if batch := readBatch(t, dir, 0, Filter{}); len(batch.Events) != 1 || batch.Next != 1 {
		t.Fatalf("partial active line was read: %#v", batch)
	}
	if reopened, err := Open(dir, Options{}); !errors.Is(err, ErrCorrupt) {
		if reopened != nil {
			reopened.Close()
		}
		t.Fatalf("Open crash-truncated file = %v", err)
	}
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReader(dir).Read(0, Filter{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("partial archived line = %v", err)
	}
	for _, contents := range []string{"not-json\n", "{}\n", strings.Repeat("x", maxRecordBytes+1) + "\n"} {
		other := t.TempDir()
		if err := os.WriteFile(filepath.Join(other, FileName), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewReader(other).Read(0, Filter{}); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("malformed file error = %v", err)
		}
	}
}

func TestMissingDirectoryInvalidOptionsAndSymlinks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if batch := readBatch(t, dir, 0, Filter{}); len(batch.Events) != 0 || batch.Next != 0 {
		t.Fatalf("missing directory = %#v", batch)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reader created its state directory")
	}
	for _, options := range []Options{{MaxBytes: -1}, {KeepFiles: -1}} {
		if w, err := Open(dir, options); err == nil {
			w.Close()
			t.Fatal("negative options accepted")
		}
	}
	if w, err := Open("", Options{}); err == nil {
		w.Close()
		t.Fatal("empty state dir accepted")
	}
	if _, err := NewReader("").Read(0, Filter{}); err == nil {
		t.Fatal("empty reader directory accepted")
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, FileName)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if w, err := Open(dir, Options{}); err == nil {
		w.Close()
		t.Fatal("writer followed an audit symlink")
	}
	if _, err := NewReader(dir).Read(0, Filter{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("reader followed audit symlink: %v", err)
	}
}
