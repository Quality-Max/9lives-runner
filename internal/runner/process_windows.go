//go:build windows

package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Start suspended, assign to a kill-on-close job, then resume. No launcher can
// spawn a child before assignment. Descendants inherit the job, including
// Chromium processes that would otherwise outlive Node.
func startAndWait(ctx context.Context, command *exec.Cmd) (error, *Termination) {
	if err := ctx.Err(); err != nil {
		return err, nil
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create owned process job: %w", err), nil
	}
	defer func() {
		if job != 0 {
			windows.CloseHandle(job)
		}
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return fmt.Errorf("configure owned process job: %w", err), nil
	}
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	command.WaitDelay = 50 * time.Millisecond
	if err := command.Start(); err != nil {
		return err, nil
	}
	abort := func(err error) (error, *Termination) {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err, nil
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		return abort(fmt.Errorf("open owned process: %w", err))
	}
	err = windows.AssignProcessToJobObject(job, process)
	windows.CloseHandle(process)
	if err != nil {
		return abort(fmt.Errorf("assign owned process job: %w", err))
	}
	if err := resumePrimaryThread(uint32(command.Process.Pid)); err != nil {
		return abort(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if errors.Is(err, exec.ErrWaitDelay) && command.ProcessState != nil && command.ProcessState.Success() {
			err = nil
		}
		active, queryErr := activeJobProcesses(job)
		if queryErr != nil {
			return errors.Join(err, queryErr), nil
		}
		if active == 0 {
			return err, nil
		}
		termination := &Termination{Kind: "cleanup", Detail: "leader exited while owned children remained"}
		return errors.Join(err, stopJob(job, termination)), termination
	case <-ctx.Done():
		termination := &Termination{Kind: "canceled", Detail: ctx.Err().Error()}
		if ctx.Err() == context.DeadlineExceeded {
			termination.Kind = "timeout"
		}
		stopErr := stopJob(job, termination)
		// Closing the last job handle also terminates descendants if explicit stop
		// failed. Never wait for a still-running command with an open job handle.
		if stopErr != nil {
			windows.CloseHandle(job)
			job = 0
			_ = command.Process.Kill()
		}
		return errors.Join(<-done, stopErr), termination
	}
}

func resumePrimaryThread(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("inspect suspended process: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return fmt.Errorf("open suspended thread: %w", err)
		}
		previous, err := windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		if err != nil {
			return fmt.Errorf("resume owned process: %w", err)
		}
		if previous != 1 {
			return fmt.Errorf("unexpected owned process suspend count %d", previous)
		}
		return nil
	}
	return fmt.Errorf("primary thread of suspended process unavailable")
}

func activeJobProcesses(job windows.Handle) (uint32, error) {
	// JOBOBJECT_BASIC_ACCOUNTING_INFORMATION (Windows SDK ABI).
	accounting := struct {
		TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
		TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
	}{}
	err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
	return accounting.ActiveProcesses, err
}

func stopJob(job windows.Handle, termination *Termination) error {
	if err := windows.TerminateJobObject(job, 1); err != nil {
		return fmt.Errorf("terminate owned process job: %w", err)
	}
	termination.Signal = "TerminateJobObject"
	deadline := time.Now().Add(2 * time.Second)
	for {
		active, err := activeJobProcesses(job)
		if err != nil {
			return fmt.Errorf("inspect process job cleanup: %w", err)
		}
		if active == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("owned process job cleanup did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
