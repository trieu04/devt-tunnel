package platform

import (
	"os"
	"path/filepath"
	"sync"
)

const maxAppLogSize = 1024 * 1024

type RotatingLog struct {
	path string
	mu   sync.Mutex
	file *os.File
}

func OpenRotatingLog(stateDir string) (*RotatingLog, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	log := &RotatingLog{path: filepath.Join(stateDir, "devt-tunnel.log")}
	if err := log.open(); err != nil {
		return nil, err
	}
	return log, nil
}

func (l *RotatingLog) open() error {
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		l.file = file
	}
	return err
}

func (l *RotatingLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if info, err := l.file.Stat(); err == nil && info.Size()+int64(len(data)) > maxAppLogSize {
		l.file.Close()
		_ = os.Remove(l.path + ".1")
		_ = os.Rename(l.path, l.path+".1")
		if err := l.open(); err != nil {
			return 0, err
		}
	}
	return l.file.Write(data)
}

func (l *RotatingLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}
