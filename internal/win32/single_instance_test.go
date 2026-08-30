package win32

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestAcquireNamedSingleInstanceMapsAlreadyExists(t *testing.T) {
	name := fmt.Sprintf(`Local\GhostDownloaderGo.Test.%d.%d`, os.Getpid(), time.Now().UnixNano())
	first, err := acquireNamedSingleInstance(name)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer first.Release()

	second, err := acquireNamedSingleInstance(name)
	if second != nil {
		second.Release()
		t.Fatal("second acquire unexpectedly returned an instance")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second acquire error = %v, want ErrAlreadyRunning", err)
	}
}

func TestAcquireNamedSingleInstanceCanBeReacquiredAfterRelease(t *testing.T) {
	name := fmt.Sprintf(`Local\GhostDownloaderGo.Test.Release.%d.%d`, os.Getpid(), time.Now().UnixNano())
	first, err := acquireNamedSingleInstance(name)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	first.Release()

	second, err := acquireNamedSingleInstance(name)
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	second.Release()
}
