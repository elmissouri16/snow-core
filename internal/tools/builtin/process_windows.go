//go:build windows

package builtin

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type managedProcess struct {
	once      sync.Once
	readyOnce sync.Once
	ready     chan struct{}
	job       windows.Handle
}

func startManagedProcess(cmd *exec.Cmd) (*managedProcess, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	managed := &managedProcess{job: job, ready: make(chan struct{})}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	// CommandContext may cancel immediately after Start. Hold cancellation until
	// the suspended process is assigned to the job, so no descendant can escape.
	cmd.Cancel = func() error { <-managed.ready; return windows.TerminateJobObject(job, 1) }
	if err := cmd.Start(); err != nil {
		managed.signalReady()
		managed.close()
		return nil, err
	}
	fail := func(cause error) (*managedProcess, error) {
		managed.signalReady()
		_ = windows.TerminateJobObject(job, 1)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		managed.close()
		return nil, cause
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fail(err)
	}
	assignErr := windows.AssignProcessToJobObject(job, process)
	_ = windows.CloseHandle(process)
	if assignErr != nil {
		return fail(assignErr)
	}
	// Assignment is complete while every thread is still suspended. Unblock any
	// pending context cancellation before resuming execution.
	managed.signalReady()
	if err := resumeProcessThreads(uint32(cmd.Process.Pid)); err != nil {
		return fail(err)
	}
	return managed, nil
}

func resumeProcessThreads(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return err
	}
	resumed := 0
	for {
		if entry.OwnerProcessID == pid {
			thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if openErr != nil {
				return openErr
			}
			_, resumeErr := windows.ResumeThread(thread)
			_ = windows.CloseHandle(thread)
			if resumeErr != nil {
				return resumeErr
			}
			resumed++
		}
		err = windows.Thread32Next(snapshot, &entry)
		if err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return err
		}
	}
	if resumed == 0 {
		return errors.New("Windows suspended process has no resumable thread")
	}
	return nil
}

func (p *managedProcess) signalReady() {
	if p != nil && p.ready != nil {
		p.readyOnce.Do(func() { close(p.ready) })
	}
}
func (p *managedProcess) close() {
	if p == nil {
		return
	}
	p.signalReady()
	p.once.Do(func() {
		if p.job != 0 {
			_ = windows.CloseHandle(p.job)
		}
	})
}
