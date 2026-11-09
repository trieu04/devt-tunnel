package audit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrBusy        = errors.New("audit: files kept rotating while taking a snapshot; retry")
	ErrCursorAhead = errors.New("audit: cursor is ahead of retained history")
)

const DefaultFollowInterval = 250 * time.Millisecond

// Batch uses the persistent event Sequence as an offset, not a byte offset or
// archive filename (both change on rotation). Next advances over ALL scanned
// records, including filtered-out ones. Lost reports that rotation has already
// discarded records after a nonzero requested offset. An initial offset of zero
// deliberately starts at the oldest retained event and does not report Lost.
type Batch struct {
	Events []Event
	Next   uint64
	Oldest uint64
	Lost   bool
}

// Reader opens short-lived snapshots, so a separate devt-tunnel audit process can follow
// a running writer through rotations. Readers do not create files/directories
// or hold open the active file between polls. Reader is safe for concurrent use.
type Reader struct{ dir string }

func NewReader(stateDir string) *Reader { return &Reader{dir: stateDir} }

// Read returns complete records after the offset in chronological append order.
// Since filters timestamps, not offsets: clock rollback does not cause records
// to be skipped or replayed. A trailing incomplete ACTIVE line is ignored until
// it completes; an incomplete archive or malformed full line is ErrCorrupt.
// Racing rotations are retried internally; sustained rotation can yield ErrBusy
// without advancing the offset. A cursor ahead of the newest retained sequence
// is ErrCursorAhead (e.g. someone replaced the state directory).
func (r *Reader) Read(after uint64, filter Filter) (Batch, error) {
	if r.dir == "" {
		return Batch{}, errors.New("audit: state directory is required")
	}
	files, err := snapshot(r.dir)
	if err != nil {
		return Batch{}, err
	}
	defer closeSnapshot(files)
	events, err := decodeSnapshot(files)
	if err != nil {
		return Batch{}, err
	}
	batch := Batch{Next: after}
	if len(events) == 0 {
		return batch, nil
	}
	batch.Oldest = events[0].Sequence
	latest := events[len(events)-1].Sequence
	if after > latest {
		return Batch{}, ErrCursorAhead
	}
	batch.Lost = after != 0 && batch.Oldest > after && batch.Oldest-after > 1
	for _, event := range events {
		if event.Sequence > after && filter.Match(event) {
			batch.Events = append(batch.Events, event)
		}
	}
	batch.Next = latest
	return batch, nil
}

// Follow implements -f. It first reads available history after the offset, then
// polls until cancellation. emit runs synchronously, only for matching events or
// Lost notifications; its error stops following. Observe Batch.Lost and warn the
// user if retention overtakes the reader: follow cannot recover deleted files.
// Zero interval uses DefaultFollowInterval. No goroutine is started by Follow.
func (r *Reader) Follow(ctx context.Context, after uint64, filter Filter, interval time.Duration, emit func(Batch) error) error {
	if emit == nil || interval < 0 {
		return errors.New("audit: callback and nonnegative follow interval are required")
	}
	if interval == 0 {
		interval = DefaultFollowInterval
	}
	timer := time.NewTicker(interval)
	defer timer.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, err := r.Read(after, filter)
		if err != nil && !errors.Is(err, ErrBusy) {
			return err
		}
		if err == nil {
			if len(batch.Events) != 0 || batch.Lost {
				if err := emit(batch); err != nil {
					return err
				}
			}
			after = batch.Next
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type fileEntry struct {
	name   string
	number int // active == 0, older archives have larger numbers
}

type snapshotFile struct {
	file  *os.File
	info  os.FileInfo
	entry fileEntry
}

func entries(dir string) ([]fileEntry, error) {
	list, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []fileEntry
	for _, item := range list {
		name := item.Name()
		if name == FileName {
			result = append(result, fileEntry{name: name})
		} else if suffix, ok := strings.CutPrefix(name, FileName+"."); ok {
			number, err := strconv.Atoi(suffix)
			if err == nil && number > 0 && strconv.Itoa(number) == suffix {
				result = append(result, fileEntry{name: name, number: number})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].number > result[j].number })
	return result, nil
}

// Open each generation, then verify that filenames still point to the same
// files and that no generation appeared/disappeared while opening. Descriptor
// reads are limited to captured sizes, excluding appends after the snapshot.
func snapshot(dir string) ([]snapshotFile, error) {
	for attempt := 0; attempt < 8; attempt++ {
		list, err := entries(dir)
		if err != nil {
			return nil, err
		}
		var files []snapshotFile
		changed := false
		for _, entry := range list {
			path := filepath.Join(dir, entry.name)
			info, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) {
				changed = true
				break
			}
			if err != nil {
				closeSnapshot(files)
				return nil, err
			}
			if !info.Mode().IsRegular() {
				closeSnapshot(files)
				return nil, fmt.Errorf("%w: expected a regular file: %s", ErrCorrupt, entry.name)
			}
			f, err := os.Open(path)
			if errors.Is(err, os.ErrNotExist) {
				changed = true
				break
			}
			if err != nil {
				closeSnapshot(files)
				return nil, err
			}
			opened, err := f.Stat()
			if err != nil {
				f.Close()
				closeSnapshot(files)
				return nil, err
			}
			files = append(files, snapshotFile{file: f, info: opened, entry: entry})
			if !os.SameFile(info, opened) {
				changed = true
				break
			}
		}
		if !changed {
			check, err := entries(dir)
			if err != nil {
				closeSnapshot(files)
				return nil, err
			}
			if len(check) != len(list) {
				changed = true
			} else {
				for i, file := range files {
					info, err := os.Lstat(filepath.Join(dir, file.entry.name))
					if check[i] != list[i] || errors.Is(err, os.ErrNotExist) ||
						(err == nil && (!os.SameFile(info, file.info) || info.Size() < file.info.Size())) {
						changed = true
						break
					}
					if err != nil {
						closeSnapshot(files)
						return nil, err
					}
				}
			}
		}
		if changed {
			closeSnapshot(files)
			continue
		}
		return files, nil
	}
	return nil, ErrBusy
}

func closeSnapshot(files []snapshotFile) {
	for i := range files {
		if files[i].file != nil {
			files[i].file.Close()
			files[i].file = nil
		}
	}
}

var errIncomplete = errors.New("incomplete line")

func splitLine(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) != 0 {
		return 0, nil, errIncomplete
	}
	return 0, nil, nil
}

func decodeSnapshot(files []snapshotFile) ([]Event, error) {
	var result []Event
	for _, file := range files {
		corrupt := func() error { return fmt.Errorf("%w: %s", ErrCorrupt, file.entry.name) }
		scanner := bufio.NewScanner(io.NewSectionReader(file.file, 0, file.info.Size()))
		scanner.Buffer(make([]byte, 4096), maxRecordBytes+1)
		scanner.Split(splitLine)
		for scanner.Scan() {
			var event Event
			decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&event); err != nil {
				return nil, corrupt()
			}
			if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
				return nil, corrupt()
			}
			if event.Sequence == 0 || event.Time.IsZero() || event.Validate() != nil {
				return nil, corrupt()
			}
			if len(result) != 0 && (result[len(result)-1].Sequence == ^uint64(0) ||
				event.Sequence != result[len(result)-1].Sequence+1) {
				return nil, corrupt()
			}
			result = append(result, event)
		}
		if err := scanner.Err(); err != nil {
			if errors.Is(err, errIncomplete) && file.entry.number == 0 {
				continue
			}
			return nil, corrupt()
		}
	}
	return result, nil
}
