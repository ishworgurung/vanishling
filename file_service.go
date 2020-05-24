package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/minio/highwayhash"
)

type fileUploader struct {
	wg sync.WaitGroup // locker
	p  string         // file storage path
	// file ttl cleaner log path; one that holds every file name that have ingress'ed
	// so they can be deleted even if the service crashed.
	lp                string
	fileUploadContext // file's upload context
}

type fileUploadContext struct {
	remoteAddr string       // file uploader IP address
	fn         string       // file uploaded name
	u          uuid.UUID    // file name: unique uuid v5
	h          hash.Hash    // file unique hash
	l          sync.RWMutex // file lock
	// file's ttl cleaner context. one file has one cleaner
	ttlCleanerContext TTLDeleteContext
	// file's ttl cleaner context. one file has one cleaner
	logTtlCleanerContext *LogBasedTTLDeleteContext
}

func newFileUploaderSvc() (*fileUploader, error) {
	//FIXME: key
	seed, err := hex.DecodeString(
		"000102030405060708090A0B0C0D0E0FF0E0D0C0B0A090807060504030201000")
	if err != nil {
		return nil, fmt.Errorf("cannot decode hex key: %v", err)
	}
	hh, err := highwayhash.New(seed)
	if err != nil {
		return nil, err
	}

	logBasedTTLCleaner := NewLogBasedTTLDeleterService()
	go logBasedTTLCleaner.StartLogCleanerTimerLoop(defaultLogPath) // read path

	return &fileUploader{
		wg: sync.WaitGroup{},
		p:  defaultStoragePath,
		lp: defaultLogPath,
		fileUploadContext: fileUploadContext{
			l:                    sync.RWMutex{},
			h:                    hh,
			ttlCleanerContext:    NewTTLDeleterService(),
			logTtlCleanerContext: logBasedTTLCleaner,
		},
	}, nil
}

func (f *fileUploader) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reqMethod := strings.ToUpper(r.Method)
	if reqMethod == http.MethodPost || reqMethod == http.MethodPut {
		f.wg.Add(1)
		go f.upload(w, r)
		f.wg.Wait()
		return
	} else if reqMethod == http.MethodGet {
		f.wg.Add(1)
		go f.download(w, r)
		f.wg.Wait()
		return
	}
	w.WriteHeader(http.StatusBadRequest)
}

func (f *fileUploader) delete(w http.ResponseWriter, r *http.Request) {
	// for audit purpose
	f.remoteAddr = r.Header.Get("X-Real-IP")
	if len(f.remoteAddr) == 0 {
		f.remoteAddr = r.RemoteAddr
	}
}

func (f *fileUploader) download(w http.ResponseWriter, r *http.Request) {
	defer f.wg.Done()
	// for audit purpose
	f.remoteAddr = r.Header.Get("X-Real-IP")
	if len(f.remoteAddr) == 0 {
		f.remoteAddr = r.RemoteAddr
	}

	fileHash := r.Header.Get(defaultFileIdHeader)
	if len(fileHash) == 0 {
		log.Printf(f.remoteAddr+": error retrieving the file '%s'", fileHash)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if strings.Contains(fileHash, "..") || strings.Contains(fileHash, "/") {
		log.Printf(f.remoteAddr+": error retrieving the file '%s'", fileHash)
		w.WriteHeader(http.StatusForbidden)
		return
	}

	//FIXME: Check that path based attacks is not possible with the code below
	p := filepath.Join(f.p, fileHash)
	// seek to the start of the uploaded file
	fileBytes, err := ioutil.ReadFile(p)
	if err != nil {
		log.Printf(f.remoteAddr+": error while reading the file: %s\n", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	log.Printf(f.remoteAddr + ": ok")
	w.WriteHeader(http.StatusOK)
	w.Write(fileBytes)
	return
}

func (f *fileUploader) upload(w http.ResponseWriter, r *http.Request) {
	defer f.wg.Done()

	// for audit purpose
	f.remoteAddr = r.Header.Get("X-Real-IP")
	if len(f.remoteAddr) == 0 {
		f.remoteAddr = r.RemoteAddr
	}

	if err := r.ParseMultipartForm(defaultMaxUploadByte); err != nil {
		log.Println(f.remoteAddr + ":" + err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	uploadedFile, handler, err := r.FormFile("file")
	if err != nil {
		log.Println(f.remoteAddr + ":" + "error retrieving the File: %s" + err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer func() {
		if err = uploadedFile.Close(); err != nil {
			log.Println(f.remoteAddr + ":" + err.Error())
		}
	}()

	if err = f.setFileName(handler.Filename, handler.Size); err != nil {
		log.Println(f.remoteAddr + ":" + err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err, fileNameAsHashId := f.hashFile(uploadedFile)
	if err != nil {
		log.Println(f.remoteAddr + ":" + err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	log.Println(f.remoteAddr + ": ok")
	w.Header().Add(defaultFileIdHeader, *fileNameAsHashId)
	w.WriteHeader(http.StatusOK)

	uploadedFileTTL := r.Header.Get(defaultTTLHeader)
	if len(uploadedFileTTL) != 0 {
		f.ttlCleanerContext.Ttl, err = time.ParseDuration(uploadedFileTTL)
		if err != nil {
			log.Printf(f.remoteAddr+":"+"invalid duration: %s", err)
			f.ttlCleanerContext.Ttl = defaultFileTTL
		}
		log.Printf(f.remoteAddr+": uploader: setting duration of %s for file deletion (ttl) for file: '%s' and hashed file id: %s",
			f.ttlCleanerContext.Ttl, f.fn, *fileNameAsHashId)
	} else {
		log.Printf(f.remoteAddr+":"+
			"uploader: setting default duration of %s for file deletion (ttl) for file: '%s' and hashed file id: %s",
			defaultFileTTL, f.fn, *fileNameAsHashId)
		f.ttlCleanerContext.Ttl = defaultFileTTL
	}

	// set ttl for deletion in the log entry in case, service goes down.
	if err := f.ttlCleanerContext.WriteLogEntry(f.ttlCleanerContext.Ttl, defaultStoragePath, *fileNameAsHashId); err != nil {
		log.Printf(f.remoteAddr+": could not write log entry for file '%s': %s\n", *fileNameAsHashId, err)
	}
}

//FIXME: Check that path based attacks is not possible with the code below
func (f *fileUploader) setFileName(fn string, fs int64) error {
	if len(fn) == 0 {
		return errors.New(f.remoteAddr + ": invalid file name")
	}
	if strings.Contains(fn, "..") || strings.Contains(fn, "/") {
		return errors.New(f.remoteAddr + ": invalid file name")
	}
	if fs == 0 {
		return errors.New(f.remoteAddr + ": zero byte file uploaded")
	}
	f.fileUploadContext.fn = fn
	return nil
}

func (f *fileUploader) ensureDirWritable() error {
	ns := uuid.NewV4().String()
	p := filepath.Join(f.p, ns)
	os.MkdirAll(f.p, 0755)
	tmp, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(p)
	return nil
}

// Hash the file and use it as a file name.
func (f *fileUploader) hashFile(uploadedFile multipart.File) (error, *string) {
	var err error
	if err := f.ensureDirWritable(); err != nil {
		return err, nil
	}
	f.l.Lock()
	defer f.l.Unlock()
	f.h.Write([]byte(time.Now().String())) // mixer
	_, err = io.Copy(f.h, uploadedFile)
	if err != nil {
		return err, nil
	}
	checksum := hex.EncodeToString(f.h.Sum(nil))
	p := filepath.Join(f.p, checksum)
	tmp, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0644)
	if err != nil {
		return err, nil
	}
	defer tmp.Close()
	// seek to the start of the uploaded file
	uploadedFile.Seek(0, io.SeekStart)
	fileBytes, err := ioutil.ReadAll(uploadedFile)
	if err != nil {
		return err, nil
	}
	_, err = tmp.Write(fileBytes)
	if err != nil {
		return err, nil
	}
	return nil, &checksum
}
