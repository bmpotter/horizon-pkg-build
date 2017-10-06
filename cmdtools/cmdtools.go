package cmdtools

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	// Version describes CLI compatibility
	Version = "0.3.0"

	// OutputInfoPrefix is a prefix for info output on stderr
	OutputInfoPrefix = "[INFO]"

	// OutputDebugPrefix is a prefix for debug output on stderr
	OutputDebugPrefix = "[DEBUG]"

	// OutputErrorPrefix is a prefix for error output on stderr
	OutputErrorPrefix = "[ERROR]"
)

// DelegateError is a subtype of error indicating an error that occured in a worker or other async process
type DelegateError struct {
	UserError bool
	Breaking  bool // indicates that the error isn't transient and stopped processing
	msg       string
}

func (e DelegateError) Error() string {
	return e.msg
}

// SynchronizedReporter is used to write messages to what would be stdout and
// stderr from multiple concurrent workers. It synchronizes access to Reader
// and Writer instances with locking and is therefore suitable only for
// accepting messages at moderate volume.
type SynchronizedReporter struct {
	ErrWriter          *io.PipeWriter
	OutWriter          *io.PipeWriter
	DelegateErrorCount int
	bufferLen          int
	pipeWatchSleep     time.Duration
	errChannel         chan DelegateError // a way for delegates to report errors from go routines
}

// NewSynchronizedReporter instantiates a SynchronizedReporter with given
// buffer length and sleep duration for each pipe watch go routine.
func NewSynchronizedReporter(bufferLen int, pipeWatchSleep time.Duration) *SynchronizedReporter {

	stderrPipeReader, stderrPipeWriter := io.Pipe()
	stdoutPipeReader, stdoutPipeWriter := io.Pipe()

	reporter := &SynchronizedReporter{
		ErrWriter:      stderrPipeWriter,
		OutWriter:      stdoutPipeWriter,
		bufferLen:      bufferLen,
		pipeWatchSleep: pipeWatchSleep,
		errChannel:     make(chan DelegateError),
	}

	go reporter.startPipeWatch(stdoutPipeReader, os.Stdout, &sync.Mutex{})
	go reporter.startPipeWatch(stderrPipeReader, os.Stderr, &sync.Mutex{})

	return reporter
}

// DelegateErrorConsumer takes a functionn for handling errors from delegates transmitted using a Reporter's ErrChannel
func (s *SynchronizedReporter) DelegateErrorConsumer(fn func(e DelegateError)) {

	go func() {
		for {
			// blocking read
			e := <-s.errChannel
			s.DelegateErrorCount++
			fn(e)
		}
	}()
}

// DelegateErr enqueues an error in the ErrChannel
func (s *SynchronizedReporter) DelegateErr(userError bool, breaking bool, msg string) {

	s.errChannel <- DelegateError{
		UserError: userError,
		Breaking:  breaking,
		msg:       msg,
	}
}

func (s *SynchronizedReporter) startPipeWatch(pipeReader *io.PipeReader, destWriter *os.File, lock *sync.Mutex) {
	defer pipeReader.Close()
	buf := make([]byte, s.bufferLen)

	for {
		lock.Lock()
		readN, err := pipeReader.Read(buf)
		if err != nil && err != io.EOF {
			fmt.Fprintf(destWriter, fmt.Sprintf("Error reading from pipereader. Error: %v\n", err), OutputErrorPrefix)
		}

		destWriter.Write(buf[0:readN])
		lock.Unlock()

		time.Sleep(s.pipeWatchSleep)
	}
}
