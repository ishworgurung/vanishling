package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type TTLDeleteContext struct {
	Tm   *time.Timer   // ttl timer
	file string        // file name
	Ttl  time.Duration // the ttl of file for deletion
}

type LogBasedTTLDeleteContext struct {
	logDeleteFunc      func() error // deleter to execute on the arg on timer expiration
	file               string       // file name
	logEntryLocker     sync.RWMutex // Log lock to synchronise write of a log entry
	logCleanerInterval *time.Ticker // Log cleaning interval ticker
}

func NewLogBasedTTLDeleterService() *LogBasedTTLDeleteContext {
	if err := os.MkdirAll(defaultLogPath, 0755); err != nil {
		log.Printf("error: %s", err)
	}
	if err := os.MkdirAll(defaultStoragePath, 0755); err != nil {
		log.Printf("error: %s", err)
	}
	return &LogBasedTTLDeleteContext{
		logEntryLocker:     sync.RWMutex{},
		logCleanerInterval: time.NewTicker(defaultLogCleanerInterval),
		logDeleteFunc: func() error {
			lsfPath := filepath.Join(defaultLogPath, defaultLogFile)
			_, err := os.OpenFile(lsfPath, os.O_RDONLY|os.O_CREATE|os.O_SYNC, 0644)
			if err != nil {
				return err
			}
			byteEntries, err := ioutil.ReadFile(lsfPath)
			if err != nil {
				log.Printf("log file read error: %s\n", err)
				return nil
			}
			entries := strings.SplitN(string(byteEntries), "\n", -1)
			for _, e := range entries {
				entrySlice := strings.Split(e, ",")
				if len(entrySlice) != 3 {
					return nil
				}
				fp := entrySlice[2]
				if _, err := os.Stat(fp); err != nil {
					// FIXME: find a way to mark file as deleted in the log entry
					continue
				}
				expirationDate, err := time.Parse(time.UnixDate, entrySlice[0])
				if err != nil {
					return errors.New("log: failed to parse UNIX date in log entry")
				}
				//fmt.Printf("%v\n", expirationDate)
				ttl, err := time.ParseDuration(entrySlice[1])
				if err != nil {
					return errors.New("log: invalid ttl in log entry")
				}

				if time.Now().Sub(expirationDate) > 0 {
					// File has expired, delete the file from filesystem.
					if err := os.Remove(fp); err == nil {
						log.Printf("log: %s deleted due to ttl expiration: %s and expiration date: %v\n", fp, ttl, expirationDate)
					}
				}
			}
			return nil
		},
	}
}

func NewTTLDeleterService() TTLDeleteContext {
	if err := os.MkdirAll(defaultLogPath, 0755); err != nil {
		log.Printf("error: %s", err)
	}
	if err := os.MkdirAll(defaultStoragePath, 0755); err != nil {
		log.Printf("error: %s", err)
	}
	return TTLDeleteContext{}
}

func (l *LogBasedTTLDeleteContext) StartLogCleanerTimerLoop(lp string) {
	for {
		select {
		case <-l.logCleanerInterval.C:
			log.Printf("log: checking log for pending deletion")
			if err := l.logDeleteFunc(); err != nil {
				log.Printf("log: '%s' could not be deleted due to error: %s", l.file, err)
			}
		}
	}
}

func (e *TTLDeleteContext) WriteLogEntry(ttl time.Duration, storagePath string, f string) error {
	lsfPath := filepath.Join(defaultLogPath, defaultLogFile)
	if _, err := os.Stat(lsfPath); err != nil {
		os.MkdirAll(defaultLogPath, 0755)
	}
	wal, err := os.OpenFile(lsfPath, os.O_RDWR|os.O_CREATE|os.O_SYNC|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer wal.Close()
	fsPath := filepath.Join(storagePath, f)
	expirationUnix := time.Now().Add(ttl).Format(time.UnixDate)
	// wal entry format: `expiration-unix-date,original-ttl,uploadedFilepath`
	le := fmt.Sprintf("%v,%v,%s\n", expirationUnix, ttl, fsPath)
	_, err = wal.WriteString(le)
	return err
}
