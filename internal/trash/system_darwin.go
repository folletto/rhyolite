//go:build darwin

package trash

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// NSFeatureUnsupportedError: the volume has no trash (network shares etc).
const nsFeatureUnsupported = 3328

var (
	macOnce sync.Once
	macErr  error
)

// System moves items to the user's Trash through NSFileManager, called
// without cgo. Finder's "Put Back" keeps working.
func System() Trasher { return Func(macTrash) }

func macInit() {
	_, macErr = purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

func macTrash(abs string) error {
	macOnce.Do(macInit)
	if macErr != nil {
		return ErrUnavailable
	}
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))

	cstr := append([]byte(abs), 0)
	nsPath := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), unsafe.Pointer(&cstr[0]))
	url := objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("fileURLWithPath:"), nsPath)
	fm := objc.ID(objc.GetClass("NSFileManager")).Send(objc.RegisterName("defaultManager"))

	var nsErr objc.ID
	// -trashItemAtURL:resultingItemURL:error: returns BOOL.
	ok := objc.Send[bool](fm, objc.RegisterName("trashItemAtURL:resultingItemURL:error:"), url, uintptr(0), unsafe.Pointer(&nsErr))
	if ok {
		return nil
	}
	if nsErr == 0 {
		return fmt.Errorf("trash failed")
	}
	code := objc.Send[int](nsErr, objc.RegisterName("code"))
	if code == nsFeatureUnsupported {
		return ErrUnavailable
	}
	desc := objc.Send[objc.ID](nsErr, objc.RegisterName("localizedDescription"))
	msg := objc.Send[string](desc, objc.RegisterName("UTF8String"))
	return fmt.Errorf("trash failed (code %d): %s", code, msg)
}
