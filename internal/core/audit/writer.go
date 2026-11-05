package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	FileName = "audit.jsonl"
	// DefaultMaxBytes matches the app log's 1 MiB rotation threshold.
	DefaultMaxBytes int64 = 1024 * 1024
	// DefaultKeepFiles counts the active file plus four numbered archives.
	DefaultKeepFiles = 5
	maxRecordBytes   = 64 * 1024
)

var (
	ErrClosed        = errors.New("audit: writer is closed")
	ErrEventTooLarge = errors.New("audit: event exceeds file size limit")
	ErrCorrupt       = errors.New("audit: malformed or incomplete audit file")
)

type Options struct {
	MaxBytes  int64            // Per-file limit; zero uses DefaultMaxBytes.
	KeepFiles int              // Active + archives; zero uses DefaultKeepFiles.
	Clock     func() time.Time // Nil uses time.Now; called under the writer lock.
}

// Writer serializes validation, sequence assignment, appends and rotation.
// Every successful Append syncs the file before returning. There is no buffered
// queue, and errors are returned rather than logged (errors may contain paths,
// but never event content). After an I/O/rotation failure it refuses more writes
// until reopened: the failed append's durability is uncertain. Rotation renames
// the active file to audit.jsonl.1 (newest), then .2, and so on. Reopening with a
// lower KeepFiles value prunes surplus archives after recovering the sequence.
//
// Open uses the directory chosen by the caller (normally platform.StateDir).
// It creates missing directories as 0700, without chmodding existing state
// directories, and creates/hardens audit files to 0600. No platform imports,
// advisory locking or OS-specific APIs are needed. The daemon's single-writer
// lifecycle lock must exclude a second Writer, even in the same process.
type Writer struct {
	mu      sync.Mutex
	path    string
	file    *os.File
	max     int64
	keep    int
	clock   func() time.Time
	size    int64
	seq     uint64
	closed  bool
	failure error
}

func Open(stateDir string, options Options) (*Writer, error) {
	if stateDir == "" {
		return nil, errors.New("audit: state directory is required")
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = DefaultMaxBytes
	}
	if options.KeepFiles == 0 {
		options.KeepFiles = DefaultKeepFiles
	}
	if options.MaxBytes < 0 || options.KeepFiles < 1 {
		return nil, errors.New("audit: rotation limits must be positive")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	w := &Writer{path: filepath.Join(stateDir, FileName), max: options.MaxBytes,
		keep: options.KeepFiles, clock: options.Clock}
	if err := w.openFile(); err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			w.file.Close()
		}
	}()
	// Refuse crash-truncated appends instead of appending to an invalid line or
	// silently rewriting/deleting audit history. An operator can archive it.
	if w.size != 0 {
		last := []byte{0}
		if _, err := w.file.ReadAt(last, w.size-1); err != nil {
			return nil, err
		}
		if last[0] != '\n' {
			return nil, ErrCorrupt
		}
	}
	files, err := snapshot(stateDir)
	if err != nil {
		return nil, err
	}
	defer closeSnapshot(files)
	for _, file := range files {
		if err := file.file.Chmod(0o600); err != nil {
			return nil, err
		}
	}
	events, err := decodeSnapshot(files)
	if err != nil {
		return nil, err
	}
	if len(events) != 0 {
		w.seq = events[len(events)-1].Sequence
	}
	// A smaller retention setting also applies to archives from earlier runs.
	// Close snapshot handles first so removal works on Windows too.
	closeSnapshot(files)
	for _, file := range files {
		if file.entry.number >= w.keep {
			if err := os.Remove(filepath.Join(stateDir, file.entry.name)); err != nil {
				return nil, err
			}
		}
	}
	ok = true
	return w, nil
}

func (w *Writer) openFile() error {
	if info, err := os.Lstat(w.path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("audit: expected a regular audit file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Read access is used only to check the last byte on startup.
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file, w.size = f, info.Size()
	return nil
}

// Append assigns Time in UTC and a durable Sequence, ignoring supplied values
// for those two fields. Result/action/decision are closed enums, not free text.
// The returned Event is the exact record written, with a copied SecretNames
// slice. An event never spans files; events bigger than MaxBytes are rejected.
func (w *Writer) Append(event Event) (Event, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return Event{}, ErrClosed
	}
	if w.failure != nil {
		return Event{}, w.failure
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	if w.seq == ^uint64(0) {
		return Event{}, errors.New("audit: sequence exhausted")
	}
	event.Sequence, event.Time = w.seq+1, w.clock().UTC()
	if event.Time.IsZero() || event.Time.Year() < 0 || event.Time.Year() > 9999 {
		return Event{}, fmt.Errorf("%w: clock", ErrInvalidEvent)
	}
	event.SecretNames = append([]string(nil), event.SecretNames...)
	data, err := json.Marshal(event)
	if err != nil {
		return Event{}, fmt.Errorf("%w: time", ErrInvalidEvent)
	}
	data = append(data, '\n')
	if int64(len(data)) > w.max || len(data) > maxRecordBytes {
		return Event{}, ErrEventTooLarge
	}
	if w.size != 0 && w.size+int64(len(data)) > w.max {
		if err := w.rotate(); err != nil {
			w.failure = err
			return Event{}, err
		}
	}
	n, err := w.file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = w.file.Sync()
	}
	if err != nil {
		w.failure = err
		return Event{}, err
	}
	w.size += int64(n)
	w.seq = event.Sequence
	return event, nil
}

func archivePath(path string, number int) string { return path + "." + strconv.Itoa(number) }

func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		w.file = nil
		return err
	}
	w.file = nil
	// Only rotation removes old history. KeepFiles includes the active file.
	oldest := w.path
	if w.keep > 1 {
		oldest = archivePath(w.path, w.keep-1)
	}
	if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for i := w.keep - 2; i >= 1; i-- {
		if err := os.Rename(archivePath(w.path, i), archivePath(w.path, i+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if w.keep > 1 {
		if err := os.Rename(w.path, archivePath(w.path, 1)); err != nil {
			return err
		}
	}
	return w.openFile()
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}
