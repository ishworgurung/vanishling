package main

import "time"

// FIXME: This should ideally come from config file
const (
	defaultStoragePath        = "/tmp/vanishling/uploads"
	defaultLogPath            = "/tmp/vanishling/log"
	defaultLogCleanerInterval = time.Second * 60
	defaultLogFile            = "entries.log"
	defaultHHSeed             = 0xffffa210 // FIXME
	defaultFileTTL            = time.Duration(time.Minute * 5)
	defaultMaxUploadByte      = 1024 * 15
	defaultFileIdHeader       = "x-file-id"
	defaultTTLHeader          = "x-ttl"
)
