package main

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <dlfcn.h>
#include <limits.h>
#include <stdlib.h>
#include <string.h>

// SecTranslocateCreateOriginalPathForURL is in the Security framework since macOS 10.12 but has no public header,
// so it is looked up at run time.
static int originalPath(const char *path, char *out, size_t outlen, CFIndex *code) {
	*code = 0;
	void *security = dlopen("/System/Library/Frameworks/Security.framework/Security", RTLD_LAZY);
	if (security == NULL) return 1;
	CFURLRef (*create)(CFURLRef, CFErrorRef *) = dlsym(security, "SecTranslocateCreateOriginalPathForURL");
	if (create == NULL) return 2;
	CFURLRef url = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)path, strlen(path), true);
	if (url == NULL) return 3;
	CFErrorRef err = NULL;
	CFURLRef original = create(url, &err);
	CFRelease(url);
	if (original == NULL) {
		if (err != NULL) {
			*code = CFErrorGetCode(err);
			CFRelease(err);
		}
		return 4;
	}
	Boolean ok = CFURLGetFileSystemRepresentation(original, true, (UInt8 *)out, outlen);
	CFRelease(original);
	return ok ? 0 : 3;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// originalPath finds where the player unzipped the app when macOS opened it from a random read-only folder (App
// Translocation). macOS has no public interface for this.
func originalPath(app string) (string, error) {
	if !strings.Contains(app, "/AppTranslocation/") {
		return app, nil
	}
	path := C.CString(app)
	defer C.free(unsafe.Pointer(path))
	out := make([]byte, C.PATH_MAX)
	var code C.CFIndex
	switch C.originalPath(path, (*C.char)(unsafe.Pointer(&out[0])), C.size_t(len(out)), &code) {
	case 0:
		return C.GoString((*C.char)(unsafe.Pointer(&out[0]))), nil
	case 1, 2:
		return "", errors.New("this macOS has no SecTranslocateCreateOriginalPathForURL")
	case 4:
		return "", fmt.Errorf("SecTranslocateCreateOriginalPathForURL: %w", syscall.Errno(code))
	default:
		return "", fmt.Errorf("%s is not a valid path", app)
	}
}
