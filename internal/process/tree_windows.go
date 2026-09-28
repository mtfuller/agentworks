//go:build windows

package process

import (
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsTree struct {
	job     windows.Handle
	process *os.Process
}

func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func attachTree(cmd *exec.Cmd) (treeController, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	processHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	err = windows.AssignProcessToJobObject(job, processHandle)
	windows.CloseHandle(processHandle)
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	return &windowsTree{job: job, process: cmd.Process}, nil
}

func (tree *windowsTree) Graceful() error {
	// Windows has no universal process-tree equivalent of SIGTERM. Give a
	// console-aware child the opportunity to handle Interrupt, then Kill handles
	// the entire Job Object when the grace period expires.
	return tree.process.Signal(os.Interrupt)
}

func (tree *windowsTree) Kill() error {
	return windows.TerminateJobObject(tree.job, 1)
}

func (tree *windowsTree) Close() error {
	return windows.CloseHandle(tree.job)
}
