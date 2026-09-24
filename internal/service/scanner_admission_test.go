package service

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"
)

func TestLocalScanAdmissionIsGlobal(t *testing.T) {
	scanner := &ScannerService{}
	finish, ok := scanner.TryBeginLocalScan()
	if !ok {
		t.Fatal("first scan rejected")
	}
	if other, ok := scanner.TryBeginLocalScan(); ok {
		other()
		t.Fatal("another library/root admitted during an active scan")
	}
	finish()
	finish, ok = scanner.TryBeginLocalScan()
	if !ok {
		t.Fatal("slot not released")
	}
	finish()
}

func TestSchedulerScanSharesGlobalAdmission(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{{"success", nil}, {"failure", errors.New("scan failed")}, {"cancelled", context.Canceled}} {
		t.Run(test.name, func(t *testing.T) {
			scanner := &ScannerService{}
			scheduler := NewSchedulerService(zap.NewNop(), nil, scanner, nil, nil)
			defer scheduler.Stop()
			entered, release := make(chan struct{}), make(chan struct{})
			scheduler.jobs = []*scheduledJob{{name: "library_scan", run: func(ctx context.Context) error {
				close(entered)
				<-release
				return test.err
			}}}
			finish, _ := scanner.TryBeginLocalScan()
			if err := scheduler.RunNowAsync(t.Context(), "library_scan"); !errors.Is(err, ErrLocalScanAlreadyRunning) {
				finish()
				close(release)
				t.Fatalf("busy scheduler admission = %v", err)
			}
			finish()
			if err := scheduler.RunNowAsync(t.Context(), "library_scan"); err != nil {
				close(release)
				t.Fatal(err)
			}
			<-entered
			if finish, ok := scanner.TryBeginLocalScan(); ok {
				finish()
				close(release)
				t.Fatal("scheduler did not reserve global scan slot")
			}
			close(release)
			scheduler.runWG.Wait()
			finish, ok := scanner.TryBeginLocalScan()
			if !ok {
				t.Fatal("scheduler did not release scan slot")
			}
			finish()
		})
	}
}

func TestSchedulerScanReleasesSlotOnShutdown(t *testing.T) {
	scanner := &ScannerService{}
	scheduler := NewSchedulerService(zap.NewNop(), nil, scanner, nil, nil)
	started := make(chan struct{})
	scheduler.jobs = []*scheduledJob{{name: "library_scan", run: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}}
	if err := scheduler.RunNowAsync(t.Context(), "library_scan"); err != nil {
		t.Fatal(err)
	}
	<-started
	scheduler.Stop()
	finish, ok := scanner.TryBeginLocalScan()
	if !ok {
		t.Fatal("shutdown retained scan slot")
	}
	finish()
}
