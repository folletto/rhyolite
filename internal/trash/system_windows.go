//go:build windows

package trash

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	shell32          = syscall.NewLazyDLL("shell32.dll")
	procFileOp       = shell32.NewProc("SHFileOperationW")
	procQueryRecycle = shell32.NewProc("SHQueryRecycleBinW")
)

const (
	foDelete          = 0x3
	fofSilent         = 0x4
	fofNoConfirmation = 0x10
	fofAllowUndo      = 0x40
	fofNoErrorUI      = 0x400
)

// shFileOpStruct matches SHFILEOPSTRUCTW on 64-bit Windows.
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

type shQueryRBInfo struct {
	cbSize      uint32
	i64Size     int64
	i64NumItems int64
}

// System sends items to the Recycle Bin. Drives without one (network shares,
// some removable drives) would be deleted permanently by SHFileOperation, so
// the bin's presence is checked first.
func System() Trasher { return Func(winTrash) }

func winTrash(abs string) error {
	vol := filepath.VolumeName(abs)
	if vol == "" {
		return ErrUnavailable
	}
	root, err := syscall.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return err
	}
	info := shQueryRBInfo{cbSize: uint32(unsafe.Sizeof(shQueryRBInfo{}))}
	if hr, _, _ := procQueryRecycle.Call(uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&info))); hr != 0 {
		return ErrUnavailable
	}
	// pFrom is a double-NUL-terminated list.
	from, err := syscall.UTF16FromString(abs)
	if err != nil {
		return err
	}
	from = append(from, 0)
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI,
	}
	if ret, _, _ := procFileOp.Call(uintptr(unsafe.Pointer(&op))); ret != 0 {
		return fmt.Errorf("recycle bin operation failed (code 0x%x)", ret)
	}
	if op.fAnyOperationsAborted != 0 {
		return fmt.Errorf("recycle bin operation aborted")
	}
	return nil
}
